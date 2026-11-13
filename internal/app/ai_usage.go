package app

import (
	"encoding/json"
	"time"

	"asimi/internal/ai"
)

type usageRange struct {
	From  time.Time `json:"from"`
	Until time.Time `json:"until"`
}

type usageBucket struct {
	ProviderID    string  `json:"providerId"`
	ProviderName  string  `json:"providerName"`
	ModelID       string  `json:"modelId"`
	Currency      string  `json:"currency"`
	Requests      int     `json:"requests"`
	InputKnown    int     `json:"inputKnown"`
	OutputKnown   int     `json:"outputKnown"`
	TotalKnown    int     `json:"totalKnown"`
	CostKnown     int     `json:"costKnown"`
	InputTokens   int64   `json:"inputTokens"`
	OutputTokens  int64   `json:"outputTokens"`
	TotalTokens   int64   `json:"totalTokens"`
	EstimatedCost float64 `json:"estimatedCost"`
}

func (r usageRange) bounds() (string, string, error) {
	if r.Until.IsZero() || !r.From.Before(r.Until) {
		return "", "", codedError("INVALID_REQUEST")
	}
	return r.From.UTC().Format(chatTimeFormat), r.Until.UTC().Format(chatTimeFormat), nil
}

func (s *Service) usageSummaryLocked(from, until time.Time) ([]usageBucket, error) {
	start, end, err := (usageRange{From: from, Until: until}).bounds()
	if err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`
WITH usage_values AS (
  SELECT r.provider_id, r.model_id,
    json_extract(u.json,'$.usage.inputTokens') AS input,
    json_extract(u.json,'$.usage.outputTokens') AS output,
    json_extract(u.json,'$.usage.totalTokens') AS total,
    NULLIF(TRIM(json_extract(u.json,'$.pricing.currency')), '') AS currency,
    json_extract(u.json,'$.estimatedCost') AS cost
  FROM ai_requests r JOIN ai_usage u ON u.request_id=r.id
  WHERE r.created_at >= ? AND r.created_at < ?
)
SELECT v.provider_id, COALESCE(json_extract(p.json,'$.name'),''), v.model_id,
  CASE WHEN v.cost IS NULL THEN '' ELSE COALESCE(v.currency,'') END AS cost_currency,
  COUNT(*), COUNT(v.input), COUNT(v.output), COUNT(v.total),
  COUNT(CASE WHEN v.currency IS NOT NULL THEN v.cost END),
  COALESCE(SUM(v.input),0), COALESCE(SUM(v.output),0), COALESCE(SUM(v.total),0),
  COALESCE(SUM(CASE WHEN v.currency IS NOT NULL THEN v.cost END),0)
FROM usage_values v LEFT JOIN ai_profiles p ON p.id=v.provider_id
GROUP BY v.provider_id, v.model_id, cost_currency
ORDER BY v.provider_id, v.model_id, cost_currency`, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []usageBucket{}
	for rows.Next() {
		var b usageBucket
		if err := rows.Scan(&b.ProviderID, &b.ProviderName, &b.ModelID, &b.Currency,
			&b.Requests, &b.InputKnown, &b.OutputKnown, &b.TotalKnown, &b.CostKnown,
			&b.InputTokens, &b.OutputTokens, &b.TotalTokens, &b.EstimatedCost); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Service) recentUsageLocked(r usageRange) ([]ai.UsageRecord, error) {
	from, until, err := r.bounds()
	if err != nil {
		return nil, err
	}
	// Bound diagnostic payloads independently of the summary's range.
	return s.queryUsageLocked(`SELECT u.json FROM ai_requests r JOIN ai_usage u ON u.request_id=r.id
WHERE r.created_at >= ? AND r.created_at < ?
ORDER BY r.created_at DESC, r.id DESC LIMIT 25`, from, until)
}

func (s *Service) requestUsageLocked(id string) ([]ai.UsageRecord, error) {
	return s.queryUsageLocked("SELECT json FROM ai_usage WHERE request_id=?", id)
}

func (s *Service) queryUsageLocked(query string, args ...any) ([]ai.UsageRecord, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ai.UsageRecord{}
	for rows.Next() {
		var raw string
		var record ai.UsageRecord
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &record); err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	return out, rows.Err()
}
