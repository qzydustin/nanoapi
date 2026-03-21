package server

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/qzydustin/nanoapi/config"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// UsageService provides usage recording and querying.
type UsageService struct {
	db *gorm.DB
}

// NewUsageService initializes the database and creates a new UsageService.
func NewUsageService(cfg config.StorageConfig) (*UsageService, error) {
	db, err := gorm.Open(sqlite.Open(cfg.DSN), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable WAL mode for better concurrent access.
	db.Exec("PRAGMA journal_mode=WAL")

	// Auto-migrate usage table.
	if err := db.AutoMigrate(&UsageRecord{}); err != nil {
		return nil, fmt.Errorf("migrate usage: %w", err)
	}

	return &UsageService{db: db}, nil
}

// RecordUsage persists a usage record. Write failures are logged but not
// propagated — they must not fail an otherwise successful model request.
func (s *UsageService) RecordUsage(rec *UsageRecord) {
	if err := s.db.Create(rec).Error; err != nil {
		slog.Error("failed to record usage", "error", err, "token_id", rec.TokenID)
	}
}

// QueryUsage returns usage records for a token within an optional time range.
func (s *UsageService) QueryUsage(tokenID string, from, to time.Time) ([]UsageRecord, error) {
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

// SummaryUsage returns an aggregated usage summary.
func (s *UsageService) SummaryUsage(tokenID string, from, to time.Time) (*UsageSummary, error) {
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
