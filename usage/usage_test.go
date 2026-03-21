package usage

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/qzydustin/nanoapi/config"
)

func setupTestService(t *testing.T) *Service {
	t.Helper()
	svc, err := NewService(config.StorageConfig{Driver: "sqlite", DSN: ":memory:"})
	if err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestRecordAndQuery(t *testing.T) {
	svc := setupTestService(t)

	rec := &UsageRecord{
		ID:                       uuid.New().String(),
		TokenID:                  "tok_1",
		Timestamp:                time.Now(),
		ClientProtocol:           "anthropic_messages",
		UpstreamProtocol:         "openai_chat",
		ClientModel:              "claude-opus-4-6",
		UpstreamModel:            "bedrock-claude-4-6-opus",
		InputTokens:              100,
		OutputTokens:             50,
		CacheCreationInputTokens: 20,
		CacheReadInputTokens:     30,
		TotalTokens:              150,
		Success:                  true,
	}
	svc.RecordUsage(rec) // non-fatal

	records, err := svc.QueryUsage("tok_1", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d", len(records))
	}
	if records[0].InputTokens != 100 {
		t.Errorf("input_tokens = %d", records[0].InputTokens)
	}
	if records[0].CacheReadInputTokens != 30 {
		t.Errorf("cache_read_input_tokens = %d", records[0].CacheReadInputTokens)
	}
}

func TestSummary(t *testing.T) {
	svc := setupTestService(t)

	for i := 0; i < 3; i++ {
		svc.RecordUsage(&UsageRecord{
			ID:                       uuid.New().String(),
			TokenID:                  "tok_1",
			Timestamp:                time.Now(),
			InputTokens:              100,
			OutputTokens:             50,
			CacheCreationInputTokens: 20,
			CacheReadInputTokens:     30,
			TotalTokens:              150,
			Success:                  true,
		})
	}

	summary, err := svc.SummaryUsage("tok_1", time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	if summary.RequestCount != 3 {
		t.Errorf("request_count = %d", summary.RequestCount)
	}
	if summary.InputTokens != 300 {
		t.Errorf("input_tokens = %d", summary.InputTokens)
	}
	if summary.CacheCreationInputTokens != 60 {
		t.Errorf("cache_creation_input_tokens = %d", summary.CacheCreationInputTokens)
	}
	if summary.CacheReadInputTokens != 90 {
		t.Errorf("cache_read_input_tokens = %d", summary.CacheReadInputTokens)
	}
	if summary.TotalTokens != 450 {
		t.Errorf("total_tokens = %d", summary.TotalTokens)
	}
}
