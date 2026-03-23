package server

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/qzydustin/nanoapi/codec"
	"github.com/qzydustin/nanoapi/config"
	"github.com/qzydustin/nanoapi/execute"
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
	usageSvc *UsageService,
	tc *TokenContext,
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
	rec := &UsageRecord{
		ID:            uuid.New().String(),
		TokenID:       tc.TokenID,
		Timestamp:     time.Now(),
		ClientModel:   clientModel,
		UpstreamModel: upstreamModel,
		Success:       success,
		ErrorCode:     errCode,
		LatencyMs:     &latencyMs,
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
	usageSvc *UsageService,
	logCfg config.LoggingConfig,
	upstreamTimeout time.Duration,
	maxBodyBytes int64,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()
		requestID := uuid.New().String()
		tc := getTokenContext(c)
		var upstreamModel string
		reqLog, reqLogErr := newDebugLog(logCfg, requestID)
		if reqLogErr != nil {
			slog.Error(formatLogLine(
				"error",
				formatLogKV("req", requestID),
				formatLogKV("message", "init request log: "+reqLogErr.Error()),
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
		if reqLog != nil {
			lines := []string{
				"method: " + c.Request.Method,
				"path: " + c.Request.URL.Path,
			}
			for k, v := range c.Request.URL.Query() {
				lines = append(lines, k+": "+strings.Join(v, ", "))
			}
			lines = append(lines, formatMapLines(formatHeadersForDebug(c.Request.Header))...)
			lines = append(lines, "body:")
			lines = append(lines, string(body))
			writeSection(reqLog,"client_request_full", lines...)
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

		originalParams := req.Params.Clone()

		hasWebSearch := false
		for _, t := range req.Tools {
			if t.Type == "web_search" {
				hasWebSearch = true
				break
			}
		}

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
			encodedBody, err = codec.EncodeOpenAIRequest(req, selection.UpstreamModel, upstreamStream, reasoningCapability)
			if err != nil {
				respondError(c, http.StatusInternalServerError, "encode request: "+err.Error())
				recordUsage(usageSvc, tc, startTime, req.ClientModel, upstreamModel, false, usageErrorCode(http.StatusInternalServerError), nil)
				return
			}
			logReasoning(requestID, req, reasoningCapability)

			// 7. Build execution plan.
			plan := &execute.ExecutionPlan{
				Mode:    mode,
				URL:     execute.BuildUpstreamURL(selection.Provider.BaseURL),
				Headers: execute.BuildHeaders(selection.Provider, upstreamStream),
				Body:    encodedBody,
				Stream:  upstreamStream,
			}
			if reqLog != nil {
				lines := []string{
					"method: POST",
					"url: " + plan.URL,
				}
				lines = append(lines, formatMapLines(plan.Headers)...)
				lines = append(lines, "body:")
				lines = append(lines, string(plan.Body))
				writeSection(reqLog,"upstream_request_full", lines...)
			}

			// 8. Execute.
			rr := executeWithRetry(c, executor, plan, upstreamTimeout, requestID, selection.Provider.Name, upstreamModel, reqLog)
			if rr.Failed {
				lastStatusCode = rr.StatusCode
				lastErrMsg = rr.ErrMsg
				if rr.StatusCode >= 500 && i < len(selections)-1 {
					slog.Info(formatLogLine("fallback",
						formatLogKV("req", requestID),
						formatLogKV("failed_provider", selection.Provider.Name),
						formatLogKV("status", rr.StatusCode),
						formatLogKV("next_provider", selections[i+1].Provider.Name),
					))
					continue
				}
				respondError(c, rr.StatusCode, rr.ErrMsg)
				recordUsage(usageSvc, tc, startTime, req.ClientModel, upstreamModel, false, usageErrorCode(rr.StatusCode), nil)
				return
			}
			result := rr.Result

			// 9. Route by execution mode.
			var outcome *handlerOutcome

			switch mode {
			case execute.ModeDirectNonStream, execute.ModeAggregateStream:
				outcome = handleNonStreamResponse(c, req.ClientModel, clientResponseID, hasWebSearch, result, reqLog)

			case execute.ModePassthroughStream:
				outcome = handlePassthroughStream(c, req.ClientModel, clientResponseID, hasWebSearch, result, reqLog)
			}

			// Cancel context after response handling (not before), so passthrough
			// streams can finish reading from the upstream connection.
			rr.Cancel()

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
					formatLogKV("debug_file", debugPath(reqLog)),
				}, formatStreamStatsKV(outcome.StreamStats)...)...,
			))
			recordUsage(usageSvc, tc, startTime, req.ClientModel, upstreamModel, success, errCode, outcome.Usage)
			return
		}

		respondError(c, lastStatusCode, lastErrMsg)
		recordUsage(usageSvc, tc, startTime, "", "", false, usageErrorCode(lastStatusCode), nil)
	}
}

const maxRetries = 3

type retryResult struct {
	Result     *execute.ExecutionResult
	Cancel     context.CancelFunc // caller must call this after consuming the result
	StatusCode int
	ErrMsg     string
	Failed     bool
}

// executeWithRetry executes the plan up to maxRetries times, retrying on 5xx.
func executeWithRetry(
	c *gin.Context,
	executor *execute.Executor,
	plan *execute.ExecutionPlan,
	upstreamTimeout time.Duration,
	requestID string,
	providerName string,
	upstreamModel string,
	reqLog *os.File,
) retryResult {
	for attempt := 1; attempt <= maxRetries; attempt++ {
		ctx, cancel := context.WithTimeout(c.Request.Context(), upstreamTimeout)
		result, err := executor.Execute(ctx, plan)
		if err == nil {
			// Do NOT cancel here — for passthrough streams the caller must
			// cancel after the response body has been fully consumed.
			return retryResult{Result: result, Cancel: cancel}
		}
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
			errMsg = sanitizeErrorBody(result.Body, statusCode)
		}

		slog.Error(formatLogLine("error", append([]string{
			formatLogKV("req", requestID),
			formatLogKV("provider", providerName),
			formatLogKV("upstream", upstreamModel),
			formatLogKV("status", statusCode),
			formatLogKV("attempt", fmt.Sprintf("%d/%d", attempt, maxRetries)),
			formatLogKV("message", truncateForLog(errMsg, 4000)),
		}, formatStreamStatsKV(streamStats)...)...))
		if reqLog != nil {
			lines := []string{fmt.Sprintf("status: %d", statusCode)}
			lines = append(lines, formatStreamStatsLines(streamStats)...)
			lines = append(lines, "body:", errMsg)
			writeSection(reqLog, "upstream_response_full", lines...)
		}

		if statusCode < 500 || attempt == maxRetries {
			return retryResult{Result: result, StatusCode: statusCode, ErrMsg: errMsg, Failed: true}
		}

		slog.Info(formatLogLine("retry",
			formatLogKV("req", requestID),
			formatLogKV("provider", providerName),
			formatLogKV("status", statusCode),
			formatLogKV("attempt", fmt.Sprintf("%d/%d", attempt, maxRetries)),
		))
	}
	panic("unreachable")
}

// sanitizeErrorBody strips HTML from upstream error bodies, extracting <h1> text if present.
func sanitizeErrorBody(body []byte, statusCode int) string {
	s := strings.TrimSpace(string(body))
	if s == "" || s[0] != '<' {
		return s
	}
	if start := strings.Index(s, "<h1>"); start != -1 {
		start += 4
		if end := strings.Index(s[start:], "</h1>"); end != -1 {
			if text := strings.TrimSpace(s[start : start+end]); text != "" {
				return text
			}
		}
	}
	return fmt.Sprintf("upstream error: status %d", statusCode)
}

func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

func formatStreamStatsKV(stats *execute.StreamStats) []string {
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
	reqLog *os.File,
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
		formatLogKV("debug_file", debugPath(reqLog)),
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
	if reasoningOut, ok := codec.BuildOpenAIReasoning(req.Params.Reasoning, capability); ok {
		outEffort = reasoningOut
	}
	var allowed []string
	if capability != nil {
		allowed = capability.AllowedEfforts
	}
	slog.Info(formatLogLine(
		"reasoning",
		formatLogKV("req", requestID),
		formatLogKV("in_mode", inMode),
		formatLogKV("in_effort", inEffort),
		formatLogKV("allowed", strings.Join(allowed, ",")),
		formatLogKV("out_effort", outEffort),
	))
}

func handleNonStreamResponse(c *gin.Context, clientModel string, clientResponseID string, hasWebSearch bool, result *execute.ExecutionResult, reqLog *os.File) *handlerOutcome {
	var resp *codec.Response
	var webSearchResults []codec.WebSearchResult

	if result.AggregatedResponse != nil {
		resp = result.AggregatedResponse
		webSearchResults = result.WebSearchResults
		if reqLog != nil {
			lines := []string{"mode: aggregate_stream"}
			lines = append(lines, formatStreamStatsLines(result.StreamStats)...)
			writeSection(reqLog,"upstream_response_full", lines...)
		}
	} else {
		var err error
		resp, err = codec.DecodeOpenAIResponse(result.Body)
		if err != nil {
			respondError(c, http.StatusBadGateway, "decode upstream response: "+err.Error())
			return &handlerOutcome{StreamStats: result.StreamStats}
		}
		webSearchResults = codec.ParseOpenWebUISources(string(result.Body))
		if reqLog != nil {
			writeSection(reqLog,
				"upstream_response_full",
				fmt.Sprintf("status: %d", result.StatusCode),
				"headers:",
			)
			writeSection(reqLog,"upstream_response_headers", formatMapLines(formatHeadersForDebug(result.Headers))...)
			writeSection(reqLog,"upstream_response_body", string(result.Body))
		}
	}

	if resp == nil {
		respondError(c, http.StatusBadGateway, "empty response")
		return &handlerOutcome{StreamStats: result.StreamStats}
	}

	injectWebSearchBlocks(resp, hasWebSearch, webSearchResults)
	codec.NormalizeResponse(resp, clientResponseID, clientModel)

	clientBody, err := codec.EncodeAnthropicClientResponse(resp)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "encode client response: "+err.Error())
		return &handlerOutcome{StreamStats: result.StreamStats}
	}

	c.Data(http.StatusOK, "application/json", clientBody)
	return &handlerOutcome{
		Usage:       resp.Usage,
		StreamStats: result.StreamStats,
	}
}

func handlePassthroughStream(c *gin.Context, clientModel string, clientResponseID string, hasWebSearch bool, result *execute.ExecutionResult, reqLog *os.File) *handlerOutcome {
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
	reqLog *os.File,
	tracker *execute.StreamStatsTracker,
) *handlerOutcome {
	var usageResult *codec.Usage
	webSearchInjected := false
	var webSearchResults []codec.WebSearchResult

	anthEnc := codec.NewAnthropicStreamEncoder()

	for scanner.Scan() {
		line := scanner.Text()
		tracker.ObserveLine(line)
		if reqLog != nil {
			writeSection(reqLog,"upstream_stream_full", line)
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
			slog.Error(formatLogLine("error", formatLogKV("message", "decode upstream stream line: "+err.Error())))
			continue
		}
		for _, ev := range events {
			if ev.Type == codec.EventUsageFinal && ev.Usage != nil {
				usageResult = ev.Usage
			}
			ev = codec.NormalizeStreamEvent(ev, clientResponseID, clientModel)
			if !webSearchInjected && hasWebSearch && ev.Type == codec.EventTextDelta && ev.Text != "" {
				// Ensure message_start is emitted before synthetic blocks.
				msgStart := anthEnc.Encode(codec.NormalizeStreamEvent(codec.StreamEvent{
					Type: codec.EventTextDelta, ResponseID: ev.ResponseID, Model: ev.Model,
				}, clientResponseID, clientModel))
				if msgStart != "" {
					fmt.Fprint(w, msgStart)
				}
				syntheticEv := codec.NormalizeStreamEvent(
					codec.SynthesizeWebSearchSSE(webSearchResults), clientResponseID, clientModel)
				if out := anthEnc.Encode(syntheticEv); out != "" {
					fmt.Fprint(w, out)
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
			}, formatStreamStatsKV(tracker.Snapshot())...)...,
		))
		if reqLog != nil {
			lines := []string{fmt.Sprintf("scan_error: %v", err)}
			lines = append(lines, formatStreamStatsLines(tracker.Snapshot())...)
			writeSection(reqLog,"upstream_stream_error", lines...)
		}
	}
	if reqLog != nil {
		lines := []string{"mode: passthrough_stream"}
		lines = append(lines, formatStreamStatsLines(tracker.Snapshot())...)
		writeSection(reqLog,"upstream_response_full", lines...)
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
	block := codec.SynthesizeWebSearchBlocks(results)
	resp.Output[0].Content = append([]codec.ContentBlock{block}, resp.Output[0].Content...)
}
