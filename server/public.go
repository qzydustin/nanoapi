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
	"github.com/qzydustin/nanoapi/config"
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
	logCfg config.LoggingConfig,
) gin.HandlerFunc {
	return func(c *gin.Context) {
		startTime := time.Now()
		requestID := uuid.New().String()
		tc := getTokenContext(c)
		var upstreamProtocol canonical.Protocol
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
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			respondError(c, clientProtocol, http.StatusBadRequest, "failed to read request body")
			recordUsage(usageSvc, tc, startTime, clientProtocol, upstreamProtocol, "", upstreamModel, false, usageErrorCode(http.StatusBadRequest), nil)
			return
		}
		if reqLog != nil && reqLog.debug {
			lines := []string{
				fmt.Sprintf("client_protocol: %s", clientProtocol),
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
		logAccess(requestID, req, selection.Provider.Name, upstreamProtocol, upstreamModel, mode, reqLog)

		// 6. Encode for upstream.
		var encodedBody []byte
		var reasoningCapability *config.ReasoningCapability
		if selection.Target != nil {
			reasoningCapability = selection.Target.Reasoning
		}
		switch upstreamProtocol {
		case canonical.ProtocolOpenAIChat:
			encodedBody, err = codec.EncodeOpenAIRequest(req, selection.UpstreamModel, upstreamStream, reasoningCapability, selection.Provider.Quirks != nil && selection.Provider.Quirks.OpenWebUIWebSearch)
		case canonical.ProtocolAnthropicMessage:
			encodedBody, err = codec.EncodeAnthropicRequest(req, selection.UpstreamModel, upstreamStream, reasoningCapability)
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
		logReasoning(requestID, req, upstreamProtocol, reasoningCapability)

		// 7. Build execution plan.
		hasWebSearch := false
		for _, t := range req.Tools {
			if t.Type == "web_search" {
				hasWebSearch = true
				break
			}
		}
		plan := &execute.ExecutionPlan{
			Mode:             mode,
			ClientProtocol:   clientProtocol,
			UpstreamProtocol: upstreamProtocol,
			URL:              execute.BuildUpstreamURL(selection.Provider.BaseURL, upstreamProtocol),
			Headers:          execute.BuildHeaders(selection.Provider, upstreamProtocol, upstreamStream),
			Body:             encodedBody,
			Stream:           upstreamStream,
			HasWebSearch:     hasWebSearch,
		}
		if reqLog != nil && reqLog.debug {
			lines := []string{
				fmt.Sprintf("upstream_protocol: %s", upstreamProtocol),
				fmt.Sprintf("method: POST"),
				fmt.Sprintf("url: %s", plan.URL),
			}
			lines = append(lines, formatMapLines(plan.Headers)...)
			lines = append(lines, "body:")
			lines = append(lines, string(plan.Body))
			reqLog.WriteSection("upstream_request_full", lines...)
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
			slog.Error(
				formatLogLine(
					"error",
					formatLogKV("req", requestID),
					formatLogKV("provider", selection.Provider.Name),
					formatLogKV("upstream", fmt.Sprintf("%s/%s", upstreamProtocol, upstreamModel)),
					formatLogKV("status", statusCode),
					formatLogKV("message", truncateForLog(errMsg, 4000)),
				),
			)
			if reqLog != nil && reqLog.debug {
				reqLog.WriteSection(
					"upstream_response_full",
					fmt.Sprintf("status: %d", statusCode),
					"body:",
					errMsg,
				)
			}
			respondError(c, clientProtocol, statusCode, errMsg)
			recordUsage(usageSvc, tc, startTime, clientProtocol, upstreamProtocol, req.ClientModel, upstreamModel, false, usageErrorCode(statusCode), nil)
			return
		}

		// 9. Route by execution mode.
		var respUsage *canonical.CanonicalUsage

		switch mode {
		case execute.ModeDirectNonStream:
			respUsage = handleDirectNonStream(c, clientProtocol, upstreamProtocol, hasWebSearch, result, reqLog)

		case execute.ModeAggregateStream:
			respUsage = handleAggregateStream(c, clientProtocol, upstreamProtocol, hasWebSearch, result, reqLog)

		case execute.ModePassthroughStream:
			respUsage = handlePassthroughStream(c, clientProtocol, upstreamProtocol, hasWebSearch, result, reqLog)
		}

		// 10. Record usage (non-fatal).
		status := c.Writer.Status()
		success := status > 0 && status < 400
		var errCode *string
		if !success {
			errCode = usageErrorCode(status)
		}
		slog.Info(formatLogLine(
			"upstream_result",
			formatLogKV("req", requestID),
			formatLogKV("status", status),
			formatLogKV("latency_ms", time.Since(startTime).Milliseconds()),
			formatLogKV("success", success),
			formatLogKV("debug_file", reqLog.DebugPath()),
		))
		recordUsage(usageSvc, tc, startTime, clientProtocol, upstreamProtocol, req.ClientModel, upstreamModel, success, errCode, respUsage)
	}
}

func truncateForLog(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

func logAccess(
	requestID string,
	req *canonical.CanonicalRequest,
	providerName string,
	upstreamProtocol canonical.Protocol,
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
		formatLogKV("client", fmt.Sprintf("%s/%s", req.ClientProtocol, req.ClientModel)),
		formatLogKV("upstream", fmt.Sprintf("%s/%s", upstreamProtocol, upstreamModel)),
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
	req *canonical.CanonicalRequest,
	upstreamProtocol canonical.Protocol,
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
	if upstreamProtocol == canonical.ProtocolOpenAIChat {
		if reasoningOut, ok := codec.DebugOpenAIReasoning(req.Params.Reasoning, capability); ok {
			outEffort = reasoningOut
		}
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

func handleDirectNonStream(c *gin.Context, clientProto, upstreamProto canonical.Protocol, hasWebSearch bool, result *execute.ExecutionResult, reqLog *requestLogger) *canonical.CanonicalUsage {
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
	if reqLog != nil && reqLog.debug {
		reqLog.WriteSection(
			"upstream_response_full",
			fmt.Sprintf("status: %d", result.StatusCode),
			"headers:",
		)
		reqLog.WriteSection("upstream_response_headers", formatMapLines(formatHeadersForDebug(result.Headers))...)
		reqLog.WriteSection("upstream_response_body", string(result.Body))
	}

	injectWebSearchBlocks(resp, hasWebSearch, clientProto, upstreamProto, codec.ParseOpenWebUISources(string(result.Body)))

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

func handleAggregateStream(c *gin.Context, clientProto, upstreamProto canonical.Protocol, hasWebSearch bool, result *execute.ExecutionResult, reqLog *requestLogger) *canonical.CanonicalUsage {
	resp := result.AggregatedResponse
	if resp == nil {
		respondError(c, clientProto, http.StatusBadGateway, "empty aggregated response")
		return nil
	}
	if reqLog != nil && reqLog.debug {
		reqLog.WriteSection("upstream_response_full", "mode: aggregate_stream")
	}

	injectWebSearchBlocks(resp, hasWebSearch, clientProto, upstreamProto, result.WebSearchResults)

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

func handlePassthroughStream(c *gin.Context, clientProto, upstreamProto canonical.Protocol, hasWebSearch bool, result *execute.ExecutionResult, reqLog *requestLogger) *canonical.CanonicalUsage {
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
	scanner.Buffer(make([]byte, 0, 64*1024), execute.MaxSSELineSize)

	switch upstreamProto {
	case canonical.ProtocolOpenAIChat:
		usageResult = streamOpenAIToClient(scanner, flusher, c.Writer, clientProto, hasWebSearch, reqLog)
	case canonical.ProtocolAnthropicMessage:
		usageResult = streamAnthropicToClient(scanner, flusher, c.Writer, clientProto, reqLog)
	}

	return usageResult
}

func streamOpenAIToClient(scanner *bufio.Scanner, flusher http.Flusher, w io.Writer, clientProto canonical.Protocol, hasWebSearch bool, reqLog *requestLogger) *canonical.CanonicalUsage {
	var usageResult *canonical.CanonicalUsage
	webSearchInjected := false
	var webSearchResults []codec.WebSearchResult

	var openaiEnc *codec.OpenAIStreamEncoder
	var anthEnc *codec.AnthropicStreamEncoder
	if clientProto == canonical.ProtocolOpenAIChat {
		openaiEnc = codec.NewOpenAIStreamEncoder()
	} else {
		anthEnc = codec.NewAnthropicStreamEncoder()
	}

	for scanner.Scan() {
		line := scanner.Text()
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
			if ev.Type == canonical.EventUsageFinal && ev.Usage != nil {
				usageResult = ev.Usage
			}
			if !webSearchInjected && hasWebSearch && anthEnc != nil && ev.Type == canonical.EventTextDelta && ev.Text != "" {
				// Ensure message_start is emitted before synthetic blocks.
				msgStart := anthEnc.Encode(canonical.CanonicalStreamEvent{
					Type: canonical.EventTextDelta, ResponseID: ev.ResponseID, Model: ev.Model,
				})
				if msgStart != "" {
					fmt.Fprint(w, msgStart)
				}
				for _, syntheticEv := range codec.SynthesizeWebSearchSSE(webSearchResults) {
					if out := anthEnc.Encode(syntheticEv); out != "" {
						fmt.Fprint(w, out)
					}
				}
				flusher.Flush()
				webSearchInjected = true
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

func streamAnthropicToClient(scanner *bufio.Scanner, flusher http.Flusher, w io.Writer, clientProto canonical.Protocol, reqLog *requestLogger) *canonical.CanonicalUsage {
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
		if reqLog != nil && reqLog.debug {
			reqLog.WriteSection("upstream_stream_full", line)
		}
		if strings.HasPrefix(line, "event: ") {
			currentEventType = strings.TrimPrefix(line, "event: ")
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			events, err := dec.DecodeLine(currentEventType, data)
			if err != nil {
				slog.Error(formatLogLine("error", formatLogKV("message", fmt.Sprintf("decode upstream stream event: %v", err))))
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
		// Anthropic error format: https://docs.anthropic.com/en/api/errors
		c.JSON(status, gin.H{
			"type":  "error",
			"error": gin.H{"type": anthropicErrorType(status), "message": msg},
		})
	default:
		// OpenAI error format: https://platform.openai.com/docs/guides/error-codes
		c.JSON(status, gin.H{
			"error": gin.H{"message": msg, "type": openAIErrorType(status)},
		})
	}
}

func openAIErrorType(status int) string {
	if status >= 500 {
		return "server_error"
	}
	return commonErrorType(status)
}

func anthropicErrorType(status int) string {
	if status >= 500 {
		return "api_error"
	}
	return commonErrorType(status)
}

func commonErrorType(status int) string {
	switch status {
	case 401:
		return "authentication_error"
	case 403:
		return "permission_error"
	case 404:
		return "not_found_error"
	case 429:
		return "rate_limit_error"
	default:
		return "invalid_request_error"
	}
}

// injectWebSearchBlocks prepends synthetic server_tool_use + web_search_tool_result
// blocks when an Anthropic client made a web_search request through an OpenAI upstream.
func injectWebSearchBlocks(resp *canonical.CanonicalResponse, hasWebSearch bool, clientProto, upstreamProto canonical.Protocol, results []codec.WebSearchResult) {
	if !hasWebSearch || clientProto != canonical.ProtocolAnthropicMessage || upstreamProto != canonical.ProtocolOpenAIChat {
		return
	}
	if len(resp.Output) == 0 {
		return
	}
	toolUse, toolResult := codec.SynthesizeWebSearchBlocks(results)
	resp.Output[0].Content = append([]canonical.CanonicalContentBlock{toolUse, toolResult}, resp.Output[0].Content...)
}
