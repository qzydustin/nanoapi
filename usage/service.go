package usage

import (
	"log/slog"
	"time"
)

// Service provides usage recording and querying.
type Service struct {
	store Store
}

// NewService creates a new usage Service.
func NewService(store Store) *Service {
	return &Service{store: store}
}

// RecordUsage persists a usage record. Write failures are logged but not
// propagated — they must not fail an otherwise successful model request.
func (s *Service) RecordUsage(rec *UsageRecord) {
	if err := s.store.Record(rec); err != nil {
		slog.Error("failed to record usage", "error", err, "token_id", rec.TokenID)
	}
}

// QueryUsage returns usage records for a token within an optional time range.
func (s *Service) QueryUsage(tokenID string, from, to time.Time) ([]UsageRecord, error) {
	return s.store.QueryByToken(tokenID, from, to)
}

// SummaryUsage returns an aggregated usage summary.
func (s *Service) SummaryUsage(tokenID string, from, to time.Time) (*UsageSummary, error) {
	return s.store.SummaryByToken(tokenID, from, to)
}
