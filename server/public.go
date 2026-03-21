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
	"github.com/qzydustin/nanoapi/codec"
	"github.com/qzydustin/nanoapi/config"
	"github.com/qzydustin/nanoapi/execute"
	"github.com/qzydustin/nanoapi/token"
	"github.com/qzydustin/nanoapi/usage"
)

func usageErrorCode(status int) *string {
	code := fmt.Sprintf("http_%d", status)
	return &code
}

type handlerOutcome struct {
	Usage       *codec.Usage
	StreamStats *execute.StreamStats
}

func recordUsage(
	usageSvc *usage.Service,
	tc *token.TokenContext,
	startTime time.Time,
	clientModel, upstreamModel string,
	success bool,
	errCode *string,
	respUsage *codec.Usage,
) {
	if tc == nil {
		return
	}

	latencyMs := time.Since(startTime).Milliseconds()
	rec := &usage.UsageRecord{
		ID:               uuid.New().String(),
		TokenID:          tc.TokenID,
		Timestamp:        time.Now(),
		ClientProtocol:   "anthropic_messages",
		UpstreamProtocol: "openai_chat",
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

// ProxyHandler returns the main proxy handler for the /v1/messages endpoint.
// The client protocol is always Anthropic Messages (Claude Code).
func ProxyHandler(
	selector *Selector,
	executor *execute.Executor,
	usageSvc *usage.Service,
	logCfg config.LoggingConfig,
	upstreamTimeout time.Duration,
	maxBodyBytes int64,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()
		requestID := uuid.New().String()
		tc := getTokenContext(c)
		var upstreamModel string
		reqLog, reqLogErr := newRequestLogger(logCfg, requestID)
		if reqLogErr != nil {
			slog.Error(formatLogLine(
				"error",
				formatLogKV("req", requestID),
				formatLogKV("message", fmt.Sprintf("init request log: %v", reqLogErr)),
			))
		}
		if reqLog != nil {
			defer reqLog.Close()
		}

		// 1. Read body.
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			status := http.StatusBadRequest
			msg := "failed to read request body"
			if err.Error() == "http: request body too large" {
				status = http.StatusRequestEntityTooLarge
				msg = "request body too large"
			}
			respondError(c, status, msg)
			recordUsage(usageSvc, tc, startTime, "", upstreamModel, false, usageErrorCode(status), nil)
			return
		}
		if reqLog != nil && reqLog.debug {
			lines := []string{
				fmt.Sprintf("method: %s", c.Request.Method),
				fmt.Sprintf("path: %s", c.Request.URL.Path),
			}
			lines = append(lines, formatQueryLines(c.Request.URL.Query())...)
			lines = append(lines, formatMapLines(formatHeadersForDebug(c.Request.Header))...)
			lines = append(lines, "body:")
			lines = append(lines, string(body))
			reqLog.WriteSection("client_request_full", lines...)
		}

		// 2. Decode request.
		req, err := codec.DecodeAnthropicRequest(body)
		if err != nil {
			respondError(c, http.StatusBadRequest, err.Error())
			recordUsage(usageSvc, tc, startTime, "", upstreamModel, false, usageErrorCode(http.StatusBadRequest), nil)
			return
		}
		clientResponseID := codec.NewAnthropicMessageID()

		// 3. Select all candidate providers (sorted by priority).
		selections, err := selector.SelectAll(req)
		if err != nil {
			respondError(c, http.StatusNotFound, err.Error())
			recordUsage(usageSvc, tc, startTime, req.ClientModel, upstreamModel, false, usageErrorCode(http.StatusNotFound), nil)
			return
		}

		// Save original params so each attempt starts from a clean state.
		originalParams := req.Params.Clone()

		hasWebSearch := false
		for _, t := range req.Tools {
			if t.Type == "web_search" {
				hasWebSearch = true
				break
			}
		}

		// Try each provider in priority order; fall back on 5xx / connection errors.
		var lastStatusCode int
		var lastErrMsg string

		for i, selection := range selections {
			// 4. Reset params and apply this provider's overrides.
			req.Params = originalParams.Clone()
			applyOverride(&req.Params, selection.Override)

			// 5. Determine execution mode.
			upstreamModel = selection.UpstreamModel
			mode := execute.DecideMode(req.Stream, selection.ForceStream)
			upstreamStream := mode == execute.ModePassthroughStream || mode == execute.ModeAggregateStream
			logAccess(requestID, req, selection.Provider.Name, upstreamModel, mode, reqLog)

			// 6. Encode for upstream.
			var encodedBody []byte
			var reasoningCapability *config.ReasoningCapability
			if selection.Target != nil {
				reasoningCapability = selection.Target.Reasoning
			}
			encodedBody, err = codec.EncodeOpenAIRequest(req, selection.UpstreamModel, upstreamStream, reasoningCapability, selection.Provider.Quirks != nil && selection.Provider.Quirks.OpenWebUIWebSearch)
			if err != nil {
				respondError(c, http.StatusInternalServerError, fmt.Sprintf("encode request: %v", err))
				recordUsage(usageSvc, tc, startTime, req.ClientModel, upstreamModel, false, usageErrorCode(http.StatusInternalServerError), nil)
				return
			}
			logReasoning(requestID, req, reasoningCapability)

			// 7. Build execution plan.
			plan := &execute.ExecutionPlan{
				Mode:         mode,
				URL:          execute.BuildUpstreamURL(selection.Provider.BaseURL),
				Headers:      execute.BuildHeaders(selection.Provider, upstreamStream),
				Body:         encodedBody,
				Stream:       upstreamStream,
				HasWebSearch: hasWebSearch,
			}
			if reqLog != nil && reqLog.debug {
				lines := []string{
					fmt.Sprintf("method: POST"),
					fmt.Sprintf("url: %s", plan.URL),
				}
				lines = append(lines, formatMapLines(plan.Headers)...)
				lines = append(lines, "body:")
				lines = append(lines, string(plan.Body))
				reqLog.WriteSection("upstream_request_full", lines...)
			}

			// 8. Execute.
			ctx, cancel := context.WithTimeout(c.Request.Context(), upstreamTimeout)
			result, err := executor.Execute(ctx, plan)
			if err != nil {
				cancel()
				statusCode := http.StatusBadGateway
				if result != nil && result.StatusCode != 0 {
					statusCode = result.StatusCode
				}
				var streamStats *execute.StreamStats
				if result != nil {
					streamStats = result.StreamStats
				}
				errMsg := err.Error()
				if result != nil && len(result.Body) > 0 {
					errMsg = string(result.Body)
				}

				slog.Error(
					formatLogLine("error", append([]string{
						formatLogKV("req", requestID),
						formatLogKV("provider", selection.Provider.Name),
						formatLogKV("upstream", fmt.Sprintf("%s", upstreamModel)),
						formatLogKV("status", statusCode),
						formatLogKV("message", truncateForLog(errMsg, 4000)),
					}, formatStreamStatsLogParts(streamStats)...)...),
				)
				if reqLog != nil && reqLog.debug {
					lines := []string{fmt.Sprintf("status: %d", statusCode)}
					lines = append(lines, formatStreamStatsLines(streamStats)...)
					lines = append(lines, "body:", errMsg)
					reqLog.WriteSection(
						"upstream_response_full",
						lines...,
					)
				}

				lastStatusCode = statusCode
				lastErrMsg = errMsg

				// Retry on 5xx or connection errors (502 from bad gateway).
				if statusCode >= 500 && i < len(selections)-1 {
					slog.Info(formatLogLine(
						"fallback",
						formatLogKV("req", requestID),
						formatLogKV("failed_provider", selection.Provider.Name),
						formatLogKV("status", statusCode),
						formatLogKV("next_provider", selections[i+1].Provider.Name),
					))
					continue
				}

				respondError(c, statusCode, errMsg)
				recordUsage(usageSvc, tc, startTime, req.ClientModel, upstreamModel, false, usageErrorCode(statusCode), nil)
				return
			}

			// 9. Route by execution mode.
			outcome := &handlerOutcome{}

			switch mode {
			case execute.ModeDirectNonStream:
				outcome = handleDirectNonStream(c, req.ClientModel, clientResponseID, hasWebSearch, result, reqLog)

			case execute.ModeAggregateStream:
				outcome = handleAggregateStream(c, req.ClientModel, clientResponseID, hasWebSearch, result, reqLog)

			case execute.ModePassthroughStream:
				outcome = handlePassthroughStream(c, req.ClientModel, clientResponseID, hasWebSearch, result, reqLog)
			}

			// Cancel context after response handling (not before), so passthrough
			// streams can finish reading from the upstream connection.
			cancel()

			// 10. Record usage (non-fatal).
			status := c.Writer.Status()
			success := status > 0 && status < 400
			var errCode *string
			if !success {
				errCode = usageErrorCode(status)
			}
			slog.Info(formatLogLine(
				"upstream_result",
				append([]string{
					formatLogKV("req", requestID),
					formatLogKV("status", status),
					formatLogKV("latency_ms", time.Since(startTime).Milliseconds()),
					formatLogKV("success", success),
					formatLogKV("debug_file", reqLog.DebugPath()),
				}, formatStreamStatsLogParts(outcome.StreamStats)...)...,
			))
			recordUsage(usageSvc, tc, startTime, req.ClientModel, upstreamModel, success, errCode, outcome.Usage)
			return
		}

		// All providers exhausted — respond with the last error.
		respondError(c, lastStatusCode, lastErrMsg)
		recordUsage(usageSvc, tc, startTime, "", "", false, usageErrorCode(lastStatusCode), nil)
	}
}

func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

func formatStreamStatsLogParts(stats *execute.StreamStats) []string {
	if stats == nil {
		return nil
	}

	var parts []string
	if stats.TTFTMs != nil {
		parts = append(parts, formatLogKV("ttft_ms", *stats.TTFTMs))
	}
	if stats.LastChunkGapMs != nil {
		parts = append(parts, formatLogKV("last_chunk_gap_ms", *stats.LastChunkGapMs))
	}
	return parts
}

func formatStreamStatsLines(stats *execute.StreamStats) []string {
	if stats == nil {
		return nil
	}

	var lines []string
	if stats.TTFTMs != nil {
		lines = append(lines, fmt.Sprintf("ttft_ms: %d", *stats.TTFTMs))
	}
	if stats.LastChunkGapMs != nil {
		lines = append(lines, fmt.Sprintf("last_chunk_gap_ms: %d", *stats.LastChunkGapMs))
	}
	return lines
}

func logAccess(
	requestID string,
	req *codec.Request,
	providerName string,
	upstreamModel string,
	mode execute.ExecutionMode,
	reqLog *requestLogger,
) {
	maxTokens := any("-")
	if req.Params.MaxTokens != nil {
		maxTokens = *req.Params.MaxTokens
	}
	parts := []string{
		formatLogKV("req", requestID),
		formatLogKV("client_model", req.ClientModel),
		formatLogKV("upstream_model", upstreamModel),
		formatLogKV("provider", providerName),
		formatLogKV("stream", req.Stream),
		formatLogKV("mode", mode),
		formatLogKV("max_tokens", maxTokens),
		formatLogKV("debug_file", reqLog.DebugPath()),
	}
	slog.Info(formatLogLine("access", parts...))
}

func logReasoning(
	requestID string,
	req *codec.Request,
	capability *config.ReasoningCapability,
) {
	if req.Params.Reasoning == nil {
		return
	}

	inMode := req.Params.Reasoning.Mode
	if inMode == "" {
		inMode = "-"
	}
	inEffort := "-"
	if req.Params.Reasoning.Effort != nil {
		inEffort = *req.Params.Reasoning.Effort
	}
	if inEffort == "" {
		inEffort = "-"
	}
	outEffort := "-"
	if reasoningOut, ok := codec.DebugOpenAIReasoning(req.Params.Reasoning, capability); ok {
		outEffort = reasoningOut
	}
	var allowed []string
	if capability != nil {
		allowed = capability.AllowedEfforts
	}
	inBudget := any("-")
	if req.Params.Reasoning.BudgetTokens != nil {
		inBudget = *req.Params.Reasoning.BudgetTokens
	}
	slog.Info(formatLogLine(
		"reasoning",
		formatLogKV("req", requestID),
		formatLogKV("in_mode", inMode),
		formatLogKV("in_effort", inEffort),
		formatLogKV("in_budget", inBudget),
		formatLogKV("allowed", strings.Join(allowed, ",")),
		formatLogKV("out_effort", outEffort),
	))
}

func handleDirectNonStream(c *gin.Context, clientModel string, clientResponseID string, hasWebSearch bool, result *execute.ExecutionResult, reqLog *requestLogger) *handlerOutcome {
	// Decode upstream response.
	resp, err := codec.DecodeOpenAIResponse(result.Body)
	if err != nil {
		respondError(c, http.StatusBadGateway, fmt.Sprintf("decode upstream response: %v", err))
		return &handlerOutcome{}
	}
	if reqLog != nil && reqLog.debug {
		reqLog.WriteSection(
			"upstream_response_full",
			fmt.Sprintf("status: %d", result.StatusCode),
			"headers:",
		)
		reqLog.WriteSection("upstream_response_headers", formatMapLines(formatHeadersForDebug(result.Headers))...)
		reqLog.WriteSection("upstream_response_body", string(result.Body))
	}

	injectWebSearchBlocks(resp, hasWebSearch, codec.ParseOpenWebUISources(string(result.Body)))
	codec.NormalizeAnthropicResponse(resp, clientResponseID, clientModel)

	// Encode for Anthropic client.
	clientBody, err := codec.EncodeAnthropicClientResponse(resp)
	if err != nil {
		respondError(c, http.StatusInternalServerError, fmt.Sprintf("encode client response: %v", err))
		return &handlerOutcome{}
	}

	c.Data(http.StatusOK, "application/json", clientBody)
	return &handlerOutcome{Usage: resp.Usage}
}

func handleAggregateStream(c *gin.Context, clientModel string, clientResponseID string, hasWebSearch bool, result *execute.ExecutionResult, reqLog *requestLogger) *handlerOutcome {
	resp := result.AggregatedResponse
	if resp == nil {
		respondError(c, http.StatusBadGateway, "empty aggregated response")
		return &handlerOutcome{StreamStats: result.StreamStats}
	}
	if reqLog != nil && reqLog.debug {
		lines := []string{"mode: aggregate_stream"}
		lines = append(lines, formatStreamStatsLines(result.StreamStats)...)
		reqLog.WriteSection("upstream_response_full", lines...)
	}

	injectWebSearchBlocks(resp, hasWebSearch, result.WebSearchResults)
	codec.NormalizeAnthropicResponse(resp, clientResponseID, clientModel)

	clientBody, err := codec.EncodeAnthropicClientResponse(resp)
	if err != nil {
		respondError(c, http.StatusInternalServerError, fmt.Sprintf("encode client response: %v", err))
		return &handlerOutcome{StreamStats: result.StreamStats}
	}

	c.Data(http.StatusOK, "application/json", clientBody)
	return &handlerOutcome{
		Usage:       resp.Usage,
		StreamStats: result.StreamStats,
	}
}

func handlePassthroughStream(c *gin.Context, clientModel string, clientResponseID string, hasWebSearch bool, result *execute.ExecutionResult, reqLog *requestLogger) *handlerOutcome {
	if result.StreamReader == nil {
		respondError(c, http.StatusBadGateway, "no stream reader")
		return &handlerOutcome{}
	}
	defer result.StreamReader.Close()

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		respondError(c, http.StatusInternalServerError, "streaming not supported")
		return &handlerOutcome{}
	}

	scanner := bufio.NewScanner(result.StreamReader)
	scanner.Buffer(make([]byte, 0, 64*1024), execute.MaxSSELineSize)

	return streamOpenAIToClient(scanner, flusher, c.Writer, clientModel, clientResponseID, hasWebSearch, reqLog, execute.NewStreamStatsTracker(result.StartedAt))
}

func streamOpenAIToClient(
	scanner *bufio.Scanner,
	flusher http.Flusher,
	w io.Writer,
	clientModel string,
	clientResponseID string,
	hasWebSearch bool,
	reqLog *requestLogger,
	tracker *execute.StreamStatsTracker,
) *handlerOutcome {
	var usageResult *codec.Usage
	webSearchInjected := false
	var webSearchResults []codec.WebSearchResult

	anthEnc := codec.NewAnthropicStreamEncoder()

	for scanner.Scan() {
		line := scanner.Text()
		tracker.ObserveLine(line)
		if reqLog != nil && reqLog.debug {
			reqLog.WriteSection("upstream_stream_full", line)
		}
		// Capture OpenWebUI sources before normal decoding.
		if hasWebSearch && !webSearchInjected && strings.HasPrefix(line, "data: ") {
			if results := codec.ParseOpenWebUISources(strings.TrimPrefix(line, "data: ")); results != nil {
				webSearchResults = results
				continue
			}
		}
		events, done, err := codec.DecodeOpenAIStreamLine(line)
		if err != nil {
			slog.Error(formatLogLine("error", formatLogKV("message", fmt.Sprintf("decode upstream stream line: %v", err))))
			continue
		}
		for _, ev := range events {
			if ev.Type == codec.EventUsageFinal && ev.Usage != nil {
				usageResult = ev.Usage
			}
			ev = codec.NormalizeAnthropicStreamEvent(ev, clientResponseID, clientModel)
			if !webSearchInjected && hasWebSearch && ev.Type == codec.EventTextDelta && ev.Text != "" {
				// Ensure message_start is emitted before synthetic blocks.
				msgStart := anthEnc.Encode(codec.NormalizeAnthropicStreamEvent(codec.StreamEvent{
					Type: codec.EventTextDelta, ResponseID: ev.ResponseID, Model: ev.Model,
				}, clientResponseID, clientModel))
				if msgStart != "" {
					fmt.Fprint(w, msgStart)
				}
				for _, syntheticEv := range codec.SynthesizeWebSearchSSE(webSearchResults) {
					syntheticEv = codec.NormalizeAnthropicStreamEvent(syntheticEv, clientResponseID, clientModel)
					if out := anthEnc.Encode(syntheticEv); out != "" {
						fmt.Fprint(w, out)
					}
				}
				flusher.Flush()
				webSearchInjected = true
			}
			if sseData := anthEnc.Encode(ev); sseData != "" {
				fmt.Fprint(w, sseData)
				flusher.Flush()
			}
		}
		if done {
			fmt.Fprint(w, anthEnc.Done())
			flusher.Flush()
			break
		}
	}
	if err := scanner.Err(); err != nil {
		slog.Error(formatLogLine(
			"stream_error",
			append([]string{
				formatLogKV("message", fmt.Sprintf("scan upstream stream: %v", err)),
			}, formatStreamStatsLogParts(tracker.Snapshot())...)...,
		))
		if reqLog != nil && reqLog.debug {
			lines := []string{fmt.Sprintf("scan_error: %v", err)}
			lines = append(lines, formatStreamStatsLines(tracker.Snapshot())...)
			reqLog.WriteSection("upstream_stream_error", lines...)
		}
	}
	if reqLog != nil && reqLog.debug {
		lines := []string{"mode: passthrough_stream"}
		lines = append(lines, formatStreamStatsLines(tracker.Snapshot())...)
		reqLog.WriteSection("upstream_response_full", lines...)
	}
	return &handlerOutcome{
		Usage:       usageResult,
		StreamStats: tracker.Snapshot(),
	}
}

func respondError(c *gin.Context, status int, msg string) {
	// Anthropic error format: https://docs.anthropic.com/en/api/errors
	c.JSON(status, gin.H{
		"type":  "error",
		"error": gin.H{"type": anthropicErrorType(status), "message": msg},
	})
}

func anthropicErrorType(status int) string {
	switch {
	case status >= 500:
		return "api_error"
	case status == 401:
		return "authentication_error"
	case status == 403:
		return "permission_error"
	case status == 404:
		return "not_found_error"
	case status == 429:
		return "rate_limit_error"
	default:
		return "invalid_request_error"
	}
}

// injectWebSearchBlocks prepends synthetic server_tool_use + web_search_tool_result
// blocks when a web_search request was made through OpenWebUI upstream.
func injectWebSearchBlocks(resp *codec.Response, hasWebSearch bool, results []codec.WebSearchResult) {
	if !hasWebSearch {
		return
	}
	if len(resp.Output) == 0 {
		return
	}
	toolUse, toolResult := codec.SynthesizeWebSearchBlocks(results)
	resp.Output[0].Content = append([]codec.ContentBlock{toolUse, toolResult}, resp.Output[0].Content...)
}
