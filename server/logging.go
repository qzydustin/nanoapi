package server

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/qzydustin/nanoapi/config"
)

func newDebugLog(cfg config.LoggingConfig, requestID string) (*os.File, error) {
	if !cfg.Debug {
		return nil, nil
	}

	if err := os.MkdirAll(cfg.RequestDir, 0o755); err != nil {
		return nil, fmt.Errorf("create request log dir: %w", err)
	}

	path := filepath.Join(cfg.RequestDir, requestID+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open request log file: %w", err)
	}
	return file, nil
}

func debugPath(f *os.File) string {
	if f == nil {
		return ""
	}
	return f.Name()
}

func writeSection(f *os.File, section string, lines ...string) {
	if f == nil {
		return
	}

	fmt.Fprintf(f, "[%s]\n", section)
	for _, line := range lines {
		if line == "" {
			continue
		}
		fmt.Fprintln(f, line)
	}
	fmt.Fprintln(f)
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

func formatHeadersForDebug(headers http.Header) map[string]string {
	if len(headers) == 0 {
		return nil
	}
	out := make(map[string]string, len(headers))
	for key, values := range headers {
		out[key] = strings.Join(values, ", ")
	}
	return out
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
