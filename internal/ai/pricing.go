package ai

// EstimateCost applies the configured rates to provider-reported token usage.
func EstimateCost(u Usage, p *Pricing) *float64 {
	if p == nil || !u.UsageAvailable || u.InputTokens == nil || u.OutputTokens == nil || *u.InputTokens < 0 || *u.OutputTokens < 0 {
		return nil
	}
	input := *u.InputTokens
	cost := float64(input)*p.InputPerMillion + float64(*u.OutputTokens)*p.OutputPerMillion
	if u.CachedInputTokens != nil {
		cached := *u.CachedInputTokens
		if cached < 0 || cached > input {
			return nil
		}
		if cached > 0 {
			if p.CachedInputPerMillion == nil {
				return nil
			}
			cost = float64(input-cached)*p.InputPerMillion +
				float64(cached)*(*p.CachedInputPerMillion) + float64(*u.OutputTokens)*p.OutputPerMillion
		}
	}
	cost /= 1_000_000
	if !finiteNonnegative(cost) {
		return nil
	}
	return &cost
}
