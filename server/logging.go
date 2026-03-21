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

type requestLogger struct {
	requestID string
	debug     bool
	file      *os.File
}

func newRequestLogger(cfg config.LoggingConfig, requestID string) (*requestLogger, error) {
	logger := &requestLogger{
		requestID: requestID,
		debug:     cfg.Debug,
	}
	if !cfg.Debug {
		return logger, nil
	}

	if err := os.MkdirAll(cfg.RequestDir, 0o755); err != nil {
		return nil, fmt.Errorf("create request log dir: %w", err)
	}

	path := filepath.Join(cfg.RequestDir, requestID+".log")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open request log file: %w", err)
	}
	logger.file = file
	return logger, nil
}

func (l *requestLogger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	return l.file.Close()
}

func (l *requestLogger) DebugPath() string {
	if l == nil || l.file == nil {
		return ""
	}
	return l.file.Name()
}

func (l *requestLogger) WriteSection(section string, lines ...string) {
	if l == nil || l.file == nil {
		return
	}

	fmt.Fprintf(l.file, "[%s]\n", section)
	for _, line := range lines {
		if line == "" {
			continue
		}
		fmt.Fprintln(l.file, line)
	}
	fmt.Fprintln(l.file)
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

	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, http.CanonicalHeaderKey(key))
	}
	slices.Sort(keys)

	out := make(map[string]string, len(keys))
	for _, key := range keys {
		values := headers.Values(key)
		switch strings.ToLower(key) {
		case "authorization":
			if len(values) > 0 {
				out[key] = redactHeaderValue(values[0])
			}
		default:
			out[key] = strings.Join(values, ", ")
		}
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
