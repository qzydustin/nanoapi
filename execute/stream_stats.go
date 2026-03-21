package execute

import (
	"strings"
	"time"
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
