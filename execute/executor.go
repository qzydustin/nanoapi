package execute

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/qzydustin/nanoapi/canonical"
	"github.com/qzydustin/nanoapi/codec"
)

// ExecutionMode describes how the gateway executes an upstream request.
type ExecutionMode string

const (
	ModePassthroughStream ExecutionMode = "passthrough_stream"
	ModeDirectNonStream   ExecutionMode = "direct_non_stream"
	ModeAggregateStream   ExecutionMode = "aggregate_stream"

	// MaxSSELineSize is the max buffer for upstream SSE lines (default 64KB is
	// too small for OpenWebUI sources events).
	MaxSSELineSize = 1024 * 1024
)

// DecideMode determines the execution mode based on client streaming
// preference and provider force-stream policy.
//
// Rule: UpstreamStream = ClientStream || ForceStream
func DecideMode(clientStream bool, forceStream bool) ExecutionMode {
	if clientStream {
		return ModePassthroughStream
	}
	if forceStream {
		return ModeAggregateStream
	}
	return ModeDirectNonStream
}

// ExecutionPlan describes a fully prepared upstream request.
type ExecutionPlan struct {
	Mode             ExecutionMode
	ClientProtocol   canonical.Protocol
	UpstreamProtocol canonical.Protocol
	URL              string
	Headers          map[string]string
	Body             []byte
	Stream           bool // actual upstream stream flag
	HasWebSearch     bool // request includes a web_search tool
}

// ExecutionResult is the outcome of executing an upstream request.
type ExecutionResult struct {
	StatusCode int
	Headers    map[string][]string

	// Non-stream result body (ModeDirectNonStream or error bodies).
	Body []byte

	// Stream reader for ModePassthroughStream.
	StreamReader io.ReadCloser

	// Aggregated response for ModeAggregateStream.
	AggregatedResponse *canonical.CanonicalResponse

	// WebSearchResults from OpenWebUI sources events (ModeAggregateStream).
	WebSearchResults []codec.WebSearchResult
}

// Executor handles upstream HTTP communication.
type Executor struct {
	client *http.Client
}

// NewExecutor creates an Executor with an HTTP/1.1 client.
// HTTP/2 multiplexing is disabled so one upstream failure cannot cancel other in-flight requests.
func NewExecutor() *Executor {
	transport := &http.Transport{
		TLSClientConfig:       &tls.Config{NextProtos: []string{"http/1.1"}},
		ForceAttemptHTTP2:     false,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
	}
	return &Executor{client: &http.Client{Transport: transport}}
}

// Execute sends the upstream request and returns the result according to the
// execution mode.
func (e *Executor) Execute(ctx context.Context, plan *ExecutionPlan) (*ExecutionResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, plan.URL, strings.NewReader(string(plan.Body)))
	if err != nil {
		return nil, fmt.Errorf("create upstream request: %w", err)
	}
	for k, v := range plan.Headers {
		req.Header.Set(k, v)
	}

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
	}

	result := &ExecutionResult{
		StatusCode: resp.StatusCode,
		Headers:    resp.Header,
	}

	// For non-2xx responses, read the error body and return.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		result.Body = body
		return result, fmt.Errorf("upstream returned status %d", resp.StatusCode)
	}

	switch plan.Mode {
	case ModeDirectNonStream:
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("read upstream response: %w", err)
		}
		result.Body = body
		return result, nil

	case ModePassthroughStream:
		result.StreamReader = resp.Body
		return result, nil

	case ModeAggregateStream:
		aggregated, searchResults, err := e.aggregateStream(plan.UpstreamProtocol, resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("aggregate stream: %w", err)
		}
		aggregated.UpstreamProtocol = plan.UpstreamProtocol
		result.AggregatedResponse = aggregated
		result.WebSearchResults = searchResults
		return result, nil

	default:
		resp.Body.Close()
		return nil, fmt.Errorf("unsupported execution mode: %s", plan.Mode)
	}
}

// aggregateStream reads an SSE stream and collects canonical events into a
// final CanonicalResponse.
func (e *Executor) aggregateStream(protocol canonical.Protocol, body io.Reader) (*canonical.CanonicalResponse, []codec.WebSearchResult, error) {
	state := &StreamAggregateState{}
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxSSELineSize)

	switch protocol {
	case canonical.ProtocolOpenAIChat:
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				if results := codec.ParseOpenWebUISources(strings.TrimPrefix(line, "data: ")); results != nil {
					state.WebSearchResults = results
					continue
				}
			}
			events, done, err := codec.DecodeOpenAIStreamLine(line)
			if err != nil {
				return nil, nil, err
			}
			for _, ev := range events {
				state.Apply(ev)
			}
			if done {
				break
			}
		}

	case canonical.ProtocolAnthropicMessage:
		dec := codec.NewAnthropicStreamDecoder()
		var currentEventType string
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				currentEventType = strings.TrimPrefix(line, "event: ")
				continue
			}
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				events, err := dec.DecodeLine(currentEventType, data)
				if err != nil {
					return nil, nil, err
				}
				for _, ev := range events {
					state.Apply(ev)
				}
				currentEventType = ""
				continue
			}
		}

	default:
		return nil, nil, fmt.Errorf("unsupported upstream protocol for aggregation: %s", protocol)
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("scan stream: %w", err)
	}

	return state.Finalize(), state.WebSearchResults, nil
}
