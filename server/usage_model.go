package server

import (
	"time"
)

type UsageRecord struct {
	ID                       string    `json:"id"`
	TokenID                  string    `json:"token_id"`
	Timestamp                time.Time `json:"timestamp"`
	ClientModel              string    `json:"client_model"`
	UpstreamModel            string    `json:"upstream_model"`
	InputTokens              int64     `json:"input_tokens"`
	OutputTokens             int64     `json:"output_tokens"`
	CacheCreationInputTokens int64     `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64     `json:"cache_read_input_tokens"`
	TotalTokens              int64     `json:"total_tokens"`
	ReasoningTokens          int64     `json:"reasoning_tokens"`
	Success                  bool      `json:"success"`
	ErrorCode                *string   `json:"error_code,omitempty"`
	LatencyMs                *int64    `json:"latency_ms,omitempty"`
}

type UsageSummary struct {
	RequestCount             int64 `json:"request_count"`
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	TotalTokens              int64 `json:"total_tokens"`
	ReasoningTokens          int64 `json:"reasoning_tokens"`
}
