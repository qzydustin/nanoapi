package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/qzydustin/nanoapi/config"
	"github.com/qzydustin/nanoapi/execute"
)

// debugLog buffers debug data in memory; flush writes to disk on errors only.
// All methods are nil-safe.
type debugLog struct {
	buf       bytes.Buffer
	path      string
	mustFlush bool
}

func newDebugLog(cfg config.LoggingConfig, requestID string) (*debugLog, error) {
	if !cfg.Debug {
		return nil, nil
	}
	if err := os.MkdirAll(cfg.RequestDir, 0o755); err != nil {
		return nil, fmt.Errorf("create request log dir: %w", err)
	}
	return &debugLog{
		path: filepath.Join(cfg.RequestDir, requestID+".log"),
	}, nil
}

func (d *debugLog) section(name string, lines ...string) {
	if d == nil {
		return
	}
	fmt.Fprintf(&d.buf, "[%s]\n", name)
	for _, line := range lines {
		if line == "" {
			continue
		}
		fmt.Fprintln(&d.buf, line)
	}
	fmt.Fprintln(&d.buf)
}

func (d *debugLog) filePath() string {
	if d == nil {
		return ""
	}
	return d.path
}

func (d *debugLog) flush() {
	if d == nil || d.buf.Len() == 0 {
		return
	}
	os.WriteFile(d.path, d.buf.Bytes(), 0o644)
}

// logStreamScanError logs stream scan errors: WARN for client disconnect, ERROR otherwise.
func logStreamScanError(err error, stats *execute.StreamStats, d *debugLog) {
	logArgs := append([]string{
		formatLogKV("message", fmt.Sprintf("scan upstream stream: %v", err)),
	}, formatStreamStatsKV(stats)...)

	if errors.Is(err, context.Canceled) {
		slog.Warn(formatLogLine("stream_client_disconnected", logArgs...))
	} else {
		slog.Error(formatLogLine("stream_error", logArgs...))
	}

	lines := []string{fmt.Sprintf("scan_error: %v", err)}
	lines = append(lines, formatStreamStatsLines(stats)...)
	d.section("upstream_stream_error", lines...)
	if d != nil {
		d.mustFlush = true
	}
}

func formatLogLine(event string, parts ...string) string {
	fields := make([]string, 0, len(parts)+1)
	fields = append(fields, event)
	for _, part := range parts {
		if part == "" {
			continue
		}
		fields = append(fields, part)
	}
	return strings.Join(fields, " | ")
}

func formatLogKV(key string, value any) string {
	return fmt.Sprintf("%s=%v", key, value)
}

func formatHeaderLines(headers http.Header) []string {
	if len(headers) == 0 {
		return nil
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		value := strings.Join(headers.Values(key), ", ")
		if strings.ToLower(key) == "authorization" {
			value = redactHeaderValue(value)
		}
		lines = append(lines, fmt.Sprintf("%s: %s", key, value))
	}
	return lines
}

func formatMapLines(values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		value := values[key]
		if strings.ToLower(key) == "authorization" {
			value = redactHeaderValue(value)
		}
		lines = append(lines, fmt.Sprintf("%s: %s", key, value))
	}
	return lines
}

func redactHeaderValue(value string) string {
	if value == "" {
		return ""
	}
	if len(value) <= 12 {
		return "[redacted]"
	}
	return value[:8] + "...[redacted]"
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
