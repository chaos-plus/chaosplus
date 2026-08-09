package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// SumCostSince totals the agent cost reported in events since ts (unix ms).
// Cost arrives inside runner event payloads (`costUsd`), so this scans the
// event log rather than a dedicated column — the log is the source of truth
// (§15.1) and today's window is small.
func (s *Store) SumCostSince(ctx context.Context, sinceMS int64) (float64, error) {
	var rows []Event
	if err := s.db.NewSelect().Model(&rows).
		Where("ts >= ?", sinceMS).
		Where("payload_json LIKE ?", "%costUsd%").
		Scan(ctx); err != nil {
		return 0, fmt.Errorf("sum cost: %w", err)
	}
	var total float64
	for _, e := range rows {
		var p struct {
			CostUSD float64 `json:"costUsd"`
			Event   struct {
				CostUSD float64 `json:"costUsd"`
			} `json:"event"`
		}
		if json.Unmarshal([]byte(e.PayloadJSON), &p) != nil {
			continue
		}
		total += p.CostUSD + p.Event.CostUSD
	}
	return total, nil
}
