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

	"github.com/qzydustin/nanoapi/codec"
	"github.com/qzydustin/nanoapi/config"
)

// StreamStats captures timing derived from an upstream SSE stream.
type StreamStats struct {
	TTFTMs         *int64
	LastChunkGapMs *int64
}

// StreamStatsTracker measures first-token latency and the gap between the last
// two meaningful upstream SSE chunks.
type StreamStatsTracker struct {
	requestStart time.Time
	firstChunkAt time.Time
	lastChunkAt  time.Time
	lastGapMs    *int64
}

// NewStreamStatsTracker creates a tracker anchored at the upstream request start.
func NewStreamStatsTracker(requestStart time.Time) *StreamStatsTracker {
	if requestStart.IsZero() {
		requestStart = time.Now()
	}
	return &StreamStatsTracker{requestStart: requestStart}
}

// ObserveLine records timing for meaningful SSE data lines.
func (t *StreamStatsTracker) ObserveLine(line string) {
	if t == nil || !isMeaningfulSSELine(line) {
		return
	}

	now := time.Now()
	if t.firstChunkAt.IsZero() {
		t.firstChunkAt = now
	}
	if !t.lastChunkAt.IsZero() {
		gapMs := now.Sub(t.lastChunkAt).Milliseconds()
		t.lastGapMs = &gapMs
	}
	t.lastChunkAt = now
}

// Snapshot returns the current stats. Nil means the stream never produced a
// meaningful SSE data line.
func (t *StreamStatsTracker) Snapshot() *StreamStats {
	if t == nil || t.firstChunkAt.IsZero() {
		return nil
	}

	ttftMs := t.firstChunkAt.Sub(t.requestStart).Milliseconds()
	return &StreamStats{
		TTFTMs:         &ttftMs,
		LastChunkGapMs: t.lastGapMs,
	}
}

func isMeaningfulSSELine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, ":") || !strings.HasPrefix(line, "data: ") {
		return false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, "data: ")) != ""
}

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
	Mode         ExecutionMode
	URL          string
	Headers      map[string]string
	Body         []byte
	Stream       bool // actual upstream stream flag
	HasWebSearch bool // request includes a web_search tool
}

// ExecutionResult is the outcome of executing an upstream request.
type ExecutionResult struct {
	StatusCode int
	Headers    map[string][]string
	StartedAt  time.Time

	// Non-stream result body (ModeDirectNonStream or error bodies).
	Body []byte

	// Stream reader for ModePassthroughStream.
	StreamReader io.ReadCloser

	// Aggregated response for ModeAggregateStream.
	AggregatedResponse *codec.Response

	// WebSearchResults from OpenWebUI sources events (ModeAggregateStream).
	WebSearchResults []codec.WebSearchResult

	// StreamStats captures timing for streamed upstream responses.
	StreamStats *StreamStats
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
	startedAt := time.Now()
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
		StartedAt:  startedAt,
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
		aggregated, searchResults, streamStats, err := e.aggregateStream(resp.Body, startedAt)
		resp.Body.Close()
		result.StreamStats = streamStats
		if err != nil {
			return result, fmt.Errorf("aggregate stream: %w", err)
		}
		result.AggregatedResponse = aggregated
		result.WebSearchResults = searchResults
		return result, nil

	default:
		resp.Body.Close()
		return nil, fmt.Errorf("unsupported execution mode: %s", plan.Mode)
	}
}

// aggregateStream reads an OpenAI SSE stream and collects events
// into a final Response.
func (e *Executor) aggregateStream(body io.Reader, startedAt time.Time) (*codec.Response, []codec.WebSearchResult, *StreamStats, error) {
	state := &StreamAggregateState{}
	tracker := NewStreamStatsTracker(startedAt)
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxSSELineSize)

	for scanner.Scan() {
		line := scanner.Text()
		tracker.ObserveLine(line)
		if strings.HasPrefix(line, "data: ") {
			if results := codec.ParseOpenWebUISources(strings.TrimPrefix(line, "data: ")); results != nil {
				state.SetWebSearchResults(results)
				continue
			}
		}
		events, done, err := codec.DecodeOpenAIStreamLine(line)
		if err != nil {
			return nil, nil, tracker.Snapshot(), err
		}
		for _, ev := range events {
			state.Apply(ev)
		}
		if done {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, tracker.Snapshot(), fmt.Errorf("scan stream: %w", err)
	}

	return state.Finalize(), state.WebSearchResults(), tracker.Snapshot(), nil
}

// BuildUpstreamURL constructs the upstream API endpoint URL.
func BuildUpstreamURL(baseURL string) string {
	for len(baseURL) > 0 && baseURL[len(baseURL)-1] == '/' {
		baseURL = baseURL[:len(baseURL)-1]
	}
	return baseURL + "/v1/chat/completions"
}

// BuildHeaders constructs the upstream request headers from provider config.
func BuildHeaders(provider *config.ProviderConfig, stream bool) map[string]string {
	headers := map[string]string{
		"Content-Type": "application/json",
	}

	if stream {
		headers["Accept"] = "text/event-stream"
	}

	headers["Authorization"] = fmt.Sprintf("Bearer %s", provider.APIKey)

	// Merge provider-level static headers.
	for k, v := range provider.Headers {
		headers[k] = v
	}

	return headers
}
