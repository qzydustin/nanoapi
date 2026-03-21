package usage

import (
	"time"
)

// UsageRecord stores per-request usage data.
type UsageRecord struct {
	ID                       string    `json:"id" gorm:"primaryKey"`
	TokenID                  string    `json:"token_id" gorm:"index"`
	Timestamp                time.Time `json:"timestamp" gorm:"index"`
	ClientProtocol           string    `json:"client_protocol"`
	UpstreamProtocol         string    `json:"upstream_protocol"`
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

// UsageSummary is an aggregated view of usage data.
type UsageSummary struct {
	RequestCount             int64 `json:"request_count"`
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	TotalTokens              int64 `json:"total_tokens"`
	ReasoningTokens          int64 `json:"reasoning_tokens"`
}
