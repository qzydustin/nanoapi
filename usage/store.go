package usage

import (
	"time"

	"gorm.io/gorm"
)

// Store is the persistence interface for usage records.
type Store interface {
	Record(rec *UsageRecord) error
	QueryByToken(tokenID string, from, to time.Time) ([]UsageRecord, error)
	SummaryByToken(tokenID string, from, to time.Time) (*UsageSummary, error)
}

// SQLiteStore implements Store using GORM.
type SQLiteStore struct {
	db *gorm.DB
}

// NewSQLiteStore creates a new usage SQLiteStore.
func NewSQLiteStore(db *gorm.DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}

func (s *SQLiteStore) Record(rec *UsageRecord) error {
	return s.db.Create(rec).Error
}

func (s *SQLiteStore) QueryByToken(tokenID string, from, to time.Time) ([]UsageRecord, error) {
	var records []UsageRecord
	q := s.db.Where("token_id = ?", tokenID)
	if !from.IsZero() {
		q = q.Where("timestamp >= ?", from)
	}
	if !to.IsZero() {
		q = q.Where("timestamp <= ?", to)
	}
	if err := q.Order("timestamp DESC").Find(&records).Error; err != nil {
		return nil, err
	}
	return records, nil
}

func (s *SQLiteStore) SummaryByToken(tokenID string, from, to time.Time) (*UsageSummary, error) {
	var summary UsageSummary
	q := s.db.Model(&UsageRecord{}).Where("token_id = ?", tokenID)
	if !from.IsZero() {
		q = q.Where("timestamp >= ?", from)
	}
	if !to.IsZero() {
		q = q.Where("timestamp <= ?", to)
	}
	err := q.Select(
		"COUNT(*) as request_count, " +
			"COALESCE(SUM(input_tokens),0) as input_tokens, " +
			"COALESCE(SUM(output_tokens),0) as output_tokens, " +
			"COALESCE(SUM(cache_creation_input_tokens),0) as cache_creation_input_tokens, " +
			"COALESCE(SUM(cache_read_input_tokens),0) as cache_read_input_tokens, " +
			"COALESCE(SUM(total_tokens),0) as total_tokens, " +
			"COALESCE(SUM(reasoning_tokens),0) as reasoning_tokens",
	).Scan(&summary).Error
	if err != nil {
		return nil, err
	}
	return &summary, nil
}
