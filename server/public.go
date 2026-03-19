package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qzydustin/nanoapi/canonical"
	"github.com/qzydustin/nanoapi/codec"
	"github.com/qzydustin/nanoapi/execute"
	"github.com/qzydustin/nanoapi/provider"
	"github.com/qzydustin/nanoapi/token"
	"github.com/qzydustin/nanoapi/usage"
)

func usageErrorCode(status int) *string {
	code := fmt.Sprintf("http_%d", status)
	return &code
}

func recordUsage(
	usageSvc *usage.Service,
	tc *token.TokenContext,
	startTime time.Time,
	clientProtocol, upstreamProtocol canonical.Protocol,
	clientModel, upstreamModel string,
	success bool,
	errCode *string,
	respUsage *canonical.CanonicalUsage,
) {
	if tc == nil {
		return
	}

	latencyMs := time.Since(startTime).Milliseconds()
	rec := &usage.UsageRecord{
		ID:               uuid.New().String(),
		TokenID:          tc.TokenID,
		Timestamp:        time.Now(),
		ClientProtocol:   string(clientProtocol),
		UpstreamProtocol: string(upstreamProtocol),
		ClientModel:      clientModel,
		UpstreamModel:    upstreamModel,
		Success:          success,
		ErrorCode:        errCode,
		LatencyMs:        &latencyMs,
	}
	if respUsage != nil {
		if respUsage.InputTokens != nil {
			rec.InputTokens = *respUsage.InputTokens
		}
		if respUsage.OutputTokens != nil {
			rec.OutputTokens = *respUsage.OutputTokens
		}
		if respUsage.CacheWriteTokens != nil {
			rec.CacheCreationInputTokens = *respUsage.CacheWriteTokens
		}
		if respUsage.CacheReadTokens != nil {
			rec.CacheReadInputTokens = *respUsage.CacheReadTokens
		}
		if respUsage.TotalTokens != nil {
			rec.TotalTokens = *respUsage.TotalTokens
		} else if respUsage.InputTokens != nil && respUsage.OutputTokens != nil {
			rec.TotalTokens = *respUsage.InputTokens + *respUsage.OutputTokens
		}
		if respUsage.ReasoningTokens != nil {
			rec.ReasoningTokens = *respUsage.ReasoningTokens
		}
	}
	usageSvc.RecordUsage(rec)
}

// ProxyHandler returns the main proxy handler for chat/messages endpoints.
func ProxyHandler(
	clientProtocol canonical.Protocol,
	selector *provider.Selector,
	executor *execute.Executor,
	usageSvc *usage.Service,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()
		tc := getTokenContext(c)
		var upstreamProtocol canonical.Protocol
		var upstreamModel string

		// 1. Read body.
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			respondError(c, clientProtocol, http.StatusBadRequest, "failed to read request body")
			recordUsage(usageSvc, tc, startTime, clientProtocol, upstreamProtocol, "", upstreamModel, false, usageErrorCode(http.StatusBadRequest), nil)
			return
		}

		// 2. Decode request.
		req, err := canonical.DecodeRequest(clientProtocol, body)
		if err != nil {
			respondError(c, clientProtocol, http.StatusBadRequest, err.Error())
			recordUsage(usageSvc, tc, startTime, clientProtocol, upstreamProtocol, "", upstreamModel, false, usageErrorCode(http.StatusBadRequest), nil)
			return
		}

		// 3. Select provider.
		selection, err := selector.Select(req)
		if err != nil {
			respondError(c, clientProtocol, http.StatusNotFound, err.Error())
			recordUsage(usageSvc, tc, startTime, clientProtocol, upstreamProtocol, req.ClientModel, upstreamModel, false, usageErrorCode(http.StatusNotFound), nil)
			return
		}

		// 4. Apply overrides.
		provider.ApplyOverride(&req.Params, selection.Override)

		// 5. Determine execution mode.
		upstreamProtocol = canonical.Protocol(selection.Provider.Protocol)
		upstreamModel = selection.UpstreamModel
		mode := execute.DecideMode(req.Stream, selection.ForceStream)
		upstreamStream := mode == execute.ModePassthroughStream || mode == execute.ModeAggregateStream

		// 6. Encode for upstream.
		var encodedBody []byte
		switch upstreamProtocol {
		case canonical.ProtocolOpenAIChat:
			encodedBody, err = codec.EncodeOpenAIRequest(req, selection.UpstreamModel, upstreamStream)
		case canonical.ProtocolAnthropicMessage:
			encodedBody, err = codec.EncodeAnthropicRequest(req, selection.UpstreamModel, upstreamStream)
		default:
			respondError(c, clientProtocol, http.StatusInternalServerError, "unsupported upstream protocol")
			recordUsage(usageSvc, tc, startTime, clientProtocol, upstreamProtocol, req.ClientModel, upstreamModel, false, usageErrorCode(http.StatusInternalServerError), nil)
			return
		}
		if err != nil {
			respondError(c, clientProtocol, http.StatusInternalServerError, fmt.Sprintf("encode request: %v", err))
			recordUsage(usageSvc, tc, startTime, clientProtocol, upstreamProtocol, req.ClientModel, upstreamModel, false, usageErrorCode(http.StatusInternalServerError), nil)
			return
		}

		// 7. Build execution plan.
		plan := &execute.ExecutionPlan{
			Mode:             mode,
			ClientProtocol:   clientProtocol,
			UpstreamProtocol: upstreamProtocol,
			URL:              execute.BuildUpstreamURL(selection.Provider.BaseURL, upstreamProtocol),
			Headers:          execute.BuildHeaders(selection.Provider, upstreamProtocol, upstreamStream),
			Body:             encodedBody,
			Stream:           upstreamStream,
		}

		// 8. Execute.
		ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
		defer cancel()
		result, err := executor.Execute(ctx, plan)
		if err != nil {
			statusCode := http.StatusBadGateway
			if result != nil && result.StatusCode != 0 {
				statusCode = result.StatusCode
			}
			errMsg := err.Error()
			if result != nil && len(result.Body) > 0 {
				errMsg = string(result.Body)
			}
			respondError(c, clientProtocol, statusCode, errMsg)
			recordUsage(usageSvc, tc, startTime, clientProtocol, upstreamProtocol, req.ClientModel, upstreamModel, false, usageErrorCode(statusCode), nil)
			return
		}

		// 9. Route by execution mode.
		var respUsage *canonical.CanonicalUsage

		switch mode {
		case execute.ModeDirectNonStream:
			respUsage = handleDirectNonStream(c, clientProtocol, upstreamProtocol, result)

		case execute.ModeAggregateStream:
			respUsage = handleAggregateStream(c, clientProtocol, result)

		case execute.ModePassthroughStream:
			respUsage = handlePassthroughStream(c, clientProtocol, upstreamProtocol, result)
		}

		// 10. Record usage (non-fatal).
		status := c.Writer.Status()
		success := status > 0 && status < 400
		var errCode *string
		if !success {
			errCode = usageErrorCode(status)
		}
		recordUsage(usageSvc, tc, startTime, clientProtocol, upstreamProtocol, req.ClientModel, upstreamModel, success, errCode, respUsage)
	}
}

func handleDirectNonStream(c *gin.Context, clientProto, upstreamProto canonical.Protocol, result *execute.ExecutionResult) *canonical.CanonicalUsage {
	// Decode upstream response.
	var resp *canonical.CanonicalResponse
	var err error
	switch upstreamProto {
	case canonical.ProtocolOpenAIChat:
		resp, err = codec.DecodeOpenAIResponse(result.Body)
	case canonical.ProtocolAnthropicMessage:
		resp, err = codec.DecodeAnthropicResponse(result.Body)
	}
	if err != nil {
		respondError(c, clientProto, http.StatusBadGateway, fmt.Sprintf("decode upstream response: %v", err))
		return nil
	}

	// Encode for client.
	var clientBody []byte
	switch clientProto {
	case canonical.ProtocolOpenAIChat:
		clientBody, err = codec.EncodeOpenAIClientResponse(resp)
	case canonical.ProtocolAnthropicMessage:
		clientBody, err = codec.EncodeAnthropicClientResponse(resp)
	}
	if err != nil {
		respondError(c, clientProto, http.StatusInternalServerError, fmt.Sprintf("encode client response: %v", err))
		return nil
	}

	c.Data(http.StatusOK, "application/json", clientBody)
	return resp.Usage
}

func handleAggregateStream(c *gin.Context, clientProto canonical.Protocol, result *execute.ExecutionResult) *canonical.CanonicalUsage {
	resp := result.AggregatedResponse
	if resp == nil {
		respondError(c, clientProto, http.StatusBadGateway, "empty aggregated response")
		return nil
	}

	var clientBody []byte
	var err error
	switch clientProto {
	case canonical.ProtocolOpenAIChat:
		clientBody, err = codec.EncodeOpenAIClientResponse(resp)
	case canonical.ProtocolAnthropicMessage:
		clientBody, err = codec.EncodeAnthropicClientResponse(resp)
	}
	if err != nil {
		respondError(c, clientProto, http.StatusInternalServerError, fmt.Sprintf("encode client response: %v", err))
		return nil
	}

	c.Data(http.StatusOK, "application/json", clientBody)
	return resp.Usage
}

func handlePassthroughStream(c *gin.Context, clientProto, upstreamProto canonical.Protocol, result *execute.ExecutionResult) *canonical.CanonicalUsage {
	if result.StreamReader == nil {
		respondError(c, clientProto, http.StatusBadGateway, "no stream reader")
		return nil
	}
	defer result.StreamReader.Close()

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		respondError(c, clientProto, http.StatusInternalServerError, "streaming not supported")
		return nil
	}

	var usageResult *canonical.CanonicalUsage
	scanner := bufio.NewScanner(result.StreamReader)

	switch upstreamProto {
	case canonical.ProtocolOpenAIChat:
		usageResult = streamOpenAIToClient(scanner, flusher, c.Writer, clientProto)
	case canonical.ProtocolAnthropicMessage:
		usageResult = streamAnthropicToClient(scanner, flusher, c.Writer, clientProto)
	}

	return usageResult
}

func streamOpenAIToClient(scanner *bufio.Scanner, flusher http.Flusher, w io.Writer, clientProto canonical.Protocol) *canonical.CanonicalUsage {
	var usageResult *canonical.CanonicalUsage

	// Choose client encoder.
	var openaiEnc *codec.OpenAIStreamEncoder
	var anthEnc *codec.AnthropicStreamEncoder
	if clientProto == canonical.ProtocolOpenAIChat {
		openaiEnc = codec.NewOpenAIStreamEncoder()
	} else {
		anthEnc = codec.NewAnthropicStreamEncoder()
	}

	for scanner.Scan() {
		line := scanner.Text()
		events, done, err := codec.DecodeOpenAIStreamLine(line)
		if err != nil {
			slog.Error("decode upstream stream line", "error", err)
			continue
		}
		for _, ev := range events {
			if ev.Type == canonical.EventUsageFinal && ev.Usage != nil {
				usageResult = ev.Usage
			}
			var sseData string
			if openaiEnc != nil {
				sseData = openaiEnc.Encode(ev)
			} else {
				sseData = anthEnc.Encode(ev)
			}
			if sseData != "" {
				fmt.Fprint(w, sseData)
				flusher.Flush()
			}
		}
		if done {
			if openaiEnc != nil {
				fmt.Fprint(w, openaiEnc.Done())
			} else {
				fmt.Fprint(w, anthEnc.Done())
			}
			flusher.Flush()
			break
		}
	}
	return usageResult
}

func streamAnthropicToClient(scanner *bufio.Scanner, flusher http.Flusher, w io.Writer, clientProto canonical.Protocol) *canonical.CanonicalUsage {
	var usageResult *canonical.CanonicalUsage
	dec := codec.NewAnthropicStreamDecoder()

	var openaiEnc *codec.OpenAIStreamEncoder
	var anthEnc *codec.AnthropicStreamEncoder
	if clientProto == canonical.ProtocolOpenAIChat {
		openaiEnc = codec.NewOpenAIStreamEncoder()
	} else {
		anthEnc = codec.NewAnthropicStreamEncoder()
	}

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
				slog.Error("decode upstream stream event", "error", err)
				currentEventType = ""
				continue
			}
			for _, ev := range events {
				if ev.Type == canonical.EventUsageFinal && ev.Usage != nil {
					usageResult = ev.Usage
				}
				var sseData string
				if openaiEnc != nil {
					sseData = openaiEnc.Encode(ev)
				} else {
					sseData = anthEnc.Encode(ev)
				}
				if sseData != "" {
					fmt.Fprint(w, sseData)
					flusher.Flush()
				}
			}
			currentEventType = ""
		}
	}

	// Final termination.
	if openaiEnc != nil {
		fmt.Fprint(w, openaiEnc.Done())
	} else {
		fmt.Fprint(w, anthEnc.Done())
	}
	flusher.Flush()

	return usageResult
}

func respondError(c *gin.Context, clientProto canonical.Protocol, status int, msg string) {
	switch clientProto {
	case canonical.ProtocolAnthropicMessage:
		c.JSON(status, gin.H{
			"type":  "error",
			"error": gin.H{"type": httpStatusToAnthropicError(status), "message": msg},
		})
	default:
		c.JSON(status, gin.H{
			"error": gin.H{"message": msg, "type": httpStatusToOpenAIError(status)},
		})
	}
}

func httpStatusToOpenAIError(status int) string {
	switch {
	case status == 401:
		return "authentication_error"
	case status == 403:
		return "permission_error"
	case status == 404:
		return "not_found_error"
	case status == 429:
		return "rate_limit_error"
	case status >= 500:
		return "server_error"
	default:
		return "invalid_request_error"
	}
}

func httpStatusToAnthropicError(status int) string {
	switch {
	case status == 401:
		return "authentication_error"
	case status == 403:
		return "permission_error"
	case status == 404:
		return "not_found_error"
	case status == 429:
		return "rate_limit_error"
	case status >= 500:
		return "api_error"
	default:
		return "invalid_request_error"
	}
}
