package server

import (
	"bufio"
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

// OpenAIModelsHandler returns the handler for the /v1/models endpoint.
func OpenAIModelsHandler(selector *Selector) gin.HandlerFunc {
	return func(c *gin.Context) {
		models := selector.ListModels()
		data := make([]gin.H, len(models))
		for i, m := range models {
			data[i] = gin.H{
				"id":       m,
				"object":   "model",
				"created":  0,
				"owned_by": "nanoapi",
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"object": "list",
			"data":   data,
		})
	}
}

// OpenAIProxyHandler returns the proxy handler for the /v1/chat/completions endpoint.
func OpenAIProxyHandler(
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
			respondOpenAIError(c, status, msg)
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
		req, err := codec.DecodeOpenAIRequest(body)
		if err != nil {
			respondOpenAIError(c, http.StatusBadRequest, err.Error())
			recordUsage(usageSvc, tc, startTime, "", upstreamModel, false, usageErrorCode(http.StatusBadRequest), nil)
			return
		}
		clientResponseID := codec.NewOpenAIResponseID()

		// 3. Select all candidate providers (sorted by priority).
		selections, err := selector.SelectAll(req)
		if err != nil {
			respondOpenAIError(c, http.StatusNotFound, err.Error())
			recordUsage(usageSvc, tc, startTime, req.ClientModel, upstreamModel, false, usageErrorCode(http.StatusNotFound), nil)
			return
		}

		originalParams := req.Params.Clone()

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
				respondOpenAIError(c, http.StatusInternalServerError, "encode request: "+err.Error())
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
				respondOpenAIError(c, rr.StatusCode, rr.ErrMsg)
				recordUsage(usageSvc, tc, startTime, req.ClientModel, upstreamModel, false, usageErrorCode(rr.StatusCode), nil)
				return
			}
			result := rr.Result

			// 9. Route by execution mode.
			var outcome *handlerOutcome

			switch mode {
			case execute.ModeDirectNonStream, execute.ModeAggregateStream:
				outcome = handleOpenAINonStreamResponse(c, req.ClientModel, clientResponseID, result, reqLog)

			case execute.ModePassthroughStream:
				outcome = handleOpenAIPassthroughStream(c, req.ClientModel, clientResponseID, result, reqLog)
			}

			// Cancel context after response handling (not before), so passthrough
			// streams can finish reading from the upstream connection.
			rr.Cancel()

			// 10. Record usage.
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

		respondOpenAIError(c, lastStatusCode, lastErrMsg)
		recordUsage(usageSvc, tc, startTime, "", "", false, usageErrorCode(lastStatusCode), nil)
	}
}

func handleOpenAINonStreamResponse(c *gin.Context, clientModel string, clientResponseID string, result *execute.ExecutionResult, reqLog *os.File) *handlerOutcome {
	var resp *codec.Response

	if result.AggregatedResponse != nil {
		resp = result.AggregatedResponse
		if reqLog != nil {
			lines := []string{"mode: aggregate_stream"}
			lines = append(lines, formatStreamStatsLines(result.StreamStats)...)
			writeSection(reqLog,"upstream_response_full", lines...)
		}
	} else {
		var err error
		resp, err = codec.DecodeOpenAIResponse(result.Body)
		if err != nil {
			respondOpenAIError(c, http.StatusBadGateway, "decode upstream response: "+err.Error())
			return &handlerOutcome{StreamStats: result.StreamStats}
		}
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
		respondOpenAIError(c, http.StatusBadGateway, "empty response")
		return &handlerOutcome{StreamStats: result.StreamStats}
	}

	codec.NormalizeResponse(resp, clientResponseID, clientModel)

	clientBody, err := codec.EncodeOpenAIClientResponse(resp)
	if err != nil {
		respondOpenAIError(c, http.StatusInternalServerError, "encode client response: "+err.Error())
		return &handlerOutcome{StreamStats: result.StreamStats}
	}

	c.Data(http.StatusOK, "application/json", clientBody)
	return &handlerOutcome{
		Usage:       resp.Usage,
		StreamStats: result.StreamStats,
	}
}

func handleOpenAIPassthroughStream(c *gin.Context, clientModel string, clientResponseID string, result *execute.ExecutionResult, reqLog *os.File) *handlerOutcome {
	if result.StreamReader == nil {
		respondOpenAIError(c, http.StatusBadGateway, "no stream reader")
		return &handlerOutcome{}
	}
	defer result.StreamReader.Close()

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		respondOpenAIError(c, http.StatusInternalServerError, "streaming not supported")
		return &handlerOutcome{}
	}

	scanner := bufio.NewScanner(result.StreamReader)
	scanner.Buffer(make([]byte, 0, 64*1024), execute.MaxSSELineSize)

	return streamOpenAIToOpenAIClient(scanner, flusher, c.Writer, clientModel, clientResponseID, reqLog, execute.NewStreamStatsTracker(result.StartedAt))
}

func streamOpenAIToOpenAIClient(
	scanner *bufio.Scanner,
	flusher http.Flusher,
	w io.Writer,
	clientModel string,
	clientResponseID string,
	reqLog *os.File,
	tracker *execute.StreamStatsTracker,
) *handlerOutcome {
	var usageResult *codec.Usage

	enc := codec.NewOpenAIStreamEncoder()

	for scanner.Scan() {
		line := scanner.Text()
		tracker.ObserveLine(line)
		if reqLog != nil {
			writeSection(reqLog,"upstream_stream_full", line)
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
			if sseData := enc.Encode(ev); sseData != "" {
				fmt.Fprint(w, sseData)
				flusher.Flush()
			}
		}
		if done {
			fmt.Fprint(w, enc.Done())
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

func respondOpenAIError(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{
		"error": gin.H{
			"message": msg,
			"type":    openAIErrorType(status),
			"code":    nil,
		},
	})
}

func openAIErrorType(status int) string {
	switch {
	case status >= 500:
		return "server_error"
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
