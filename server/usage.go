package server

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"sync"
	"time"
)

type UsageService struct {
	mu      sync.RWMutex
	records []UsageRecord
	file    *os.File
}

func NewUsageService(path string) (*UsageService, error) {
	var records []UsageRecord

	if f, err := os.Open(path); err == nil {
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			var rec UsageRecord
			if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
				continue // skip malformed lines
			}
			records = append(records, rec)
		}
		f.Close()
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("open usage file: %w", err)
	}

	return &UsageService{records: records, file: file}, nil
}

func (s *UsageService) RecordUsage(rec *UsageRecord) {
	data, err := json.Marshal(rec)
	if err != nil {
		slog.Error("failed to marshal usage record", "error", err, "token_id", rec.TokenID)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.records = append(s.records, *rec)
	if _, err := s.file.Write(append(data, '\n')); err != nil {
		slog.Error("failed to write usage record", "error", err, "token_id", rec.TokenID)
	}
}

func (s *UsageService) QueryUsage(tokenID string, from, to time.Time) ([]UsageRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []UsageRecord
	for _, r := range s.records {
		if r.TokenID != tokenID {
			continue
		}
		if !from.IsZero() && r.Timestamp.Before(from) {
			continue
		}
		if !to.IsZero() && r.Timestamp.After(to) {
			continue
		}
		results = append(results, r)
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Timestamp.After(results[j].Timestamp)
	})
	return results, nil
}

func (s *UsageService) SummaryUsage(tokenID string, from, to time.Time) (*UsageSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var summary UsageSummary
	for _, r := range s.records {
		if r.TokenID != tokenID {
			continue
		}
		if !from.IsZero() && r.Timestamp.Before(from) {
			continue
		}
		if !to.IsZero() && r.Timestamp.After(to) {
			continue
		}
		summary.RequestCount++
		summary.InputTokens += r.InputTokens
		summary.OutputTokens += r.OutputTokens
		summary.CacheCreationInputTokens += r.CacheCreationInputTokens
		summary.CacheReadInputTokens += r.CacheReadInputTokens
		summary.TotalTokens += r.TotalTokens
		summary.ReasoningTokens += r.ReasoningTokens
	}
	return &summary, nil
}
