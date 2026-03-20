package proxy

import (
	"strings"

	"x-tool/internal/config"
)

type resolvedSoftToolPromptConfig struct {
	ProfileID string
	Protocol  string
	Template  string
}

func (a *App) resolveSoftToolPromptConfig(upstream config.UpstreamService) resolvedSoftToolPromptConfig {
	resolved := resolvedSoftToolPromptConfig{
		Protocol: config.SoftToolProtocolXML,
	}

	cfg := a.Config()
	profileID := strings.TrimSpace(upstream.SoftToolPromptProfileID)
	if profileID == "" && cfg != nil {
		profileID = strings.TrimSpace(cfg.Features.DefaultSoftToolPromptProfileID)
	}

	var profile *config.SoftToolPromptProfile
	if profileID != "" && cfg != nil {
		if found, ok := findEnabledSoftToolPromptProfile(cfg.SoftToolPromptProfiles, profileID); ok {
			profile = &found
			resolved.ProfileID = found.ID
		}
	}

	protocolName := ""
	if profile != nil {
		protocolName = strings.TrimSpace(profile.Protocol)
	}
	if protocolName == "" {
		protocolName = strings.TrimSpace(upstream.SoftToolProtocol)
	}
	if protocolName == "" && cfg != nil {
		protocolName = strings.TrimSpace(cfg.Features.SoftToolProtocol)
	}
	if normalized, ok := config.NormalizeSoftToolProtocol(protocolName); ok {
		resolved.Protocol = normalized
	}

	if profile != nil && strings.TrimSpace(profile.Template) != "" {
		resolved.Template = profile.Template
	} else if cfg != nil {
		resolved.Template = cfg.Features.PromptTemplate
	}

	return resolved
}

func findEnabledSoftToolPromptProfile(profiles []config.SoftToolPromptProfile, id string) (config.SoftToolPromptProfile, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return config.SoftToolPromptProfile{}, false
	}

	for _, profile := range profiles {
		if !profile.Enabled {
			continue
		}
		if strings.TrimSpace(profile.ID) == id {
			return profile, true
		}
	}
	return config.SoftToolPromptProfile{}, false
}
