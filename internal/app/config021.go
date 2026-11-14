package app

import (
	"database/sql"
	"encoding/json"

	"asimi/internal/ai"
	"asimi/internal/configuration"
	"asimi/internal/domain"
)

func (s *Service) configurationPreferences() (configuration.Preferences, error) {
	var raw string
	var stored configuration.Preferences
	if err := s.db.QueryRow("SELECT json FROM ai_settings WHERE id='ai'").Scan(&raw); err != nil {
		return stored, err
	}
	if json.Unmarshal([]byte(raw), &stored) != nil {
		return stored, codedError("CONFIGURATION_INVALID")
	}
	return stored, nil
}

func (s *Service) resolvedConfiguration() (configuration.Resolved, error) {
	stored, err := s.configurationPreferences()
	if err != nil {
		return configuration.Resolved{}, err
	}
	return configuration.Resolve(stored)
}

func (s *Service) writeConfiguration(v configuration.Preferences) (configuration.Resolved, error) {
	resolved, err := configuration.Resolve(v)
	if err != nil {
		return resolved, err
	}
	err = s.transaction(func(tx *sql.Tx) error {
		if _, err := tx.Exec("UPDATE ai_settings SET json=? WHERE id='ai'", encode(v)); err != nil {
			return err
		}
		return audit(tx, "", "CONFIGURATION_UPDATED", map[string]string{"scope": "ai-preferences"})
	})
	return resolved, err
}

// Preserve explicit 0.2 values and independent Agent preferences when the
// existing provider or advanced settings forms update a selection.
func (s *Service) saveResolvedAISettings(v ai.Selection) (ai.Selection, error) {
	stored, err := s.configurationPreferences()
	if err != nil {
		return ai.Selection{}, err
	}
	base, err := configuration.Resolve(configuration.Preferences{})
	if err != nil {
		return ai.Selection{}, err
	}
	if stored.MaxOutputTokens == 0 && v.MaxOutputTokens == base.Selection.MaxOutputTokens {
		v.MaxOutputTokens = 0
	}
	if stored.MaxContextBytes == 0 && v.MaxContextBytes == base.Selection.MaxContextBytes {
		v.MaxContextBytes = 0
	}
	stored.Selection = v
	resolved, err := s.writeConfiguration(stored)
	return resolved.Selection, err
}

func setResolvedDefaultModelLocked(tx *sql.Tx, providerID, modelID string) error {
	_, err := tx.Exec(`UPDATE ai_settings SET json=json_set(json,'$.defaultProviderId',?,'$.defaultModelId',?) WHERE id='ai'`, providerID, modelID)
	return err
}

func (s *Service) callConfig021(method, payload string) (domain.Reply, bool) {
	switch method {
	case "GetConfiguration", "SaveAgentPreferences", "RestoreConfigurationDefaults":
	default:
		return domain.Reply{}, false
	}
	done := func(value any, err error) (domain.Reply, bool) { return result(value, err), true }
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.lifetime.Err() != nil {
		return fail("SERVICE_CLOSED"), true
	}
	if method == "GetConfiguration" {
		if err := decode(payload, &struct{}{}); err != nil {
			return done(nil, err)
		}
		v, err := s.resolvedConfiguration()
		if err == nil {
			settings, readErr := s.settingsLocked()
			if readErr != nil {
				return done(nil, readErr)
			}
			if settings.AppLanguage != "en-US" {
				v.Sources["ui.language"] = configuration.UserDefined
			}
			if settings.Theme != "light" {
				v.Sources["ui.theme"] = configuration.UserDefined
			}
			if v.Selection.DefaultProviderID != "" {
				profile, readErr := s.aiProfileLocked(v.Selection.DefaultProviderID)
				if readErr == nil {
					for _, preset := range providerPresets() {
						if !matchesPresetEndpoint(profile, preset.Profile) {
							continue
						}
						snapshot, readErr := s.presetProviderLocked(preset, v.Selection.DefaultModelID)
						if readErr == nil && snapshot.state.ProviderID == profile.ID && snapshot.state.VerifiedAt != "" {
							v.Sources["provider.selection"] = configuration.Preset
							for _, model := range preset.Profile.Models {
								if model.ID == v.Selection.DefaultModelID && encode(model.Capabilities) == encode(snapshot.state.ConfiguredModel.Capabilities) {
									v.Sources["model.capabilities"] = configuration.Preset
									break
								}
							}
						}
						break
					}
				}
			}
		}
		return done(v, err)
	}
	if method == "RestoreConfigurationDefaults" {
		var q struct {
			Confirm bool `json:"confirm"`
		}
		if err := decode(payload, &q); err != nil {
			return done(nil, err)
		}
		if !q.Confirm {
			return fail("CONFIRMATION_REQUIRED"), true
		}
		stored, err := s.configurationPreferences()
		if err != nil && errorCode(err) != "CONFIGURATION_INVALID" {
			return done(nil, err)
		}
		if err != nil {
			stored = configuration.Preferences{}
		}
		stored.Agent = nil
		stored.Media = nil
		stored.MaxContextBytes = 0
		stored.MaxOutputTokens = 0
		stored.WarningThreshold = nil
		value, err := s.writeConfiguration(stored)
		return done(value, err)
	}
	var q struct {
		Limits configuration.AgentLimits  `json:"limits"`
		Media  *configuration.MediaLimits `json:"media,omitempty"`
	}
	if err := decode(payload, &q); err != nil {
		return done(nil, err)
	}
	stored, err := s.configurationPreferences()
	if err != nil {
		return done(nil, err)
	}
	stored.Agent = &q.Limits
	if q.Media != nil {
		stored.Media = q.Media
	}
	value, err := s.writeConfiguration(stored)
	return done(value, err)
}
