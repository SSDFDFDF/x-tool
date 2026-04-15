package config

import "testing"

func TestEnsureBuiltInSoftToolPromptProfilesAddsClaudeCodeNativeProfile(t *testing.T) {
	cfg := &AppConfig{}

	EnsureBuiltInSoftToolPromptProfiles(cfg)

	if len(cfg.SoftToolPromptProfiles) == 0 {
		t.Fatalf("expected built-in prompt profiles to be added")
	}

	profile := cfg.SoftToolPromptProfiles[0]
	if profile.ID != BuiltInSoftToolPromptProfileClaudeCodeNativeID {
		t.Fatalf("expected first built-in profile id %q, got %q", BuiltInSoftToolPromptProfileClaudeCodeNativeID, profile.ID)
	}
	if !profile.Enabled {
		t.Fatalf("expected built-in profile to be enabled")
	}
	if profile.Template == "" {
		t.Fatalf("expected built-in profile template to be populated")
	}
}

func TestMergeBuiltInSoftToolPromptProfilesPreservesExistingBuiltInOverride(t *testing.T) {
	existing := []SoftToolPromptProfile{{
		ID:          BuiltInSoftToolPromptProfileClaudeCodeNativeID,
		Name:        "Custom Native",
		Description: "custom",
		Protocol:    SoftToolProtocolMarkdownBlock,
		Template:    "{tool_catalog}\n{single_call_example}",
		Enabled:     false,
	}}

	merged := MergeBuiltInSoftToolPromptProfiles(existing)

	if len(merged) != 1 {
		t.Fatalf("expected existing built-in override to be preserved without duplicates, got %d profiles", len(merged))
	}
	if merged[0].Name != "Custom Native" {
		t.Fatalf("expected existing built-in override to be kept, got %q", merged[0].Name)
	}
	if merged[0].Enabled {
		t.Fatalf("expected existing built-in override fields to be preserved")
	}
}

func TestValidateAllowsEmptyUpstreamServices(t *testing.T) {
	cfg := &AppConfig{
		Server: ServerConfig{
			Port:    8000,
			Host:    "0.0.0.0",
			Timeout: 180,
		},
		UpstreamServices: nil,
		Features: FeaturesConfig{
			LogLevel:       "INFO",
			PromptTemplate: "{tool_catalog}\n{trigger_signal}",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected empty upstream services to be valid, got %v", err)
	}
}

func TestValidateAllowsSameModelAcrossDifferentClientKeys(t *testing.T) {
	cfg := &AppConfig{
		Server: ServerConfig{
			Port:    8000,
			Host:    "0.0.0.0",
			Timeout: 180,
		},
		UpstreamServices: []UpstreamService{
			{
				Name:       "openai-a",
				BaseURL:    "https://a.example.com/v1",
				APIKey:     "key-a",
				IsDefault:  true,
				Models:     []string{"gpt-4o"},
				ClientKeys: []string{"client-a"},
			},
			{
				Name:       "openai-b",
				BaseURL:    "https://b.example.com/v1",
				APIKey:     "key-b",
				Models:     []string{"gpt-4o"},
				ClientKeys: []string{"client-b"},
			},
		},
		Features: FeaturesConfig{
			LogLevel:       "INFO",
			PromptTemplate: "{tool_catalog}\n{trigger_signal}",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected duplicate model across different client keys to be valid, got %v", err)
	}
}

func TestValidateRejectsSameModelForSameClientKeyAcrossUpstreams(t *testing.T) {
	cfg := &AppConfig{
		Server: ServerConfig{
			Port:    8000,
			Host:    "0.0.0.0",
			Timeout: 180,
		},
		UpstreamServices: []UpstreamService{
			{
				Name:       "openai-a",
				BaseURL:    "https://a.example.com/v1",
				APIKey:     "key-a",
				IsDefault:  true,
				Models:     []string{"gpt-4o"},
				ClientKeys: []string{"shared-client"},
			},
			{
				Name:       "openai-b",
				BaseURL:    "https://b.example.com/v1",
				APIKey:     "key-b",
				Models:     []string{"gpt-4o"},
				ClientKeys: []string{"shared-client"},
			},
		},
		Features: FeaturesConfig{
			LogLevel:       "INFO",
			PromptTemplate: "{tool_catalog}\n{trigger_signal}",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected validation to reject duplicate model for the same client key")
	}
}

func TestValidateAllowsSameModelForSameClientKeyAcrossProtocols(t *testing.T) {
	cfg := &AppConfig{
		Server: ServerConfig{
			Port:    8000,
			Host:    "0.0.0.0",
			Timeout: 180,
		},
		UpstreamServices: []UpstreamService{
			{
				Name:             "openai-a",
				BaseURL:          "https://a.example.com/v1",
				APIKey:           "key-a",
				IsDefault:        true,
				Models:           []string{"gpt-4o"},
				ClientKeys:       []string{"shared-client"},
				UpstreamProtocol: UpstreamProtocolOpenAICompat,
			},
			{
				Name:             "anthropic-a",
				BaseURL:          "https://b.example.com/v1",
				APIKey:           "key-b",
				Models:           []string{"gpt-4o"},
				ClientKeys:       []string{"shared-client"},
				UpstreamProtocol: UpstreamProtocolAnthropic,
			},
		},
		Features: FeaturesConfig{
			LogLevel:       "INFO",
			PromptTemplate: "{tool_catalog}\n{trigger_signal}",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected duplicate model across protocols to be valid, got %v", err)
	}
}

func TestValidateAllowsPromptTemplateWithProtocolFragments(t *testing.T) {
	cfg := &AppConfig{
		Server: ServerConfig{
			Port:    8000,
			Host:    "0.0.0.0",
			Timeout: 180,
		},
		Features: FeaturesConfig{
			LogLevel:       "INFO",
			PromptTemplate: "{tool_catalog}\n{protocol_rules}\n{output_rules}",
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected fragment-based prompt template to be valid, got %v", err)
	}
}

func TestValidateNormalizesPromptInjectionTarget(t *testing.T) {
	cfg := &AppConfig{
		Server: ServerConfig{
			Port:    8000,
			Host:    "0.0.0.0",
			Timeout: 180,
		},
		Features: FeaturesConfig{
			LogLevel:              "INFO",
			PromptTemplate:        "{tool_catalog}\n{protocol_rules}\n{output_rules}",
			PromptInjectionTarget: "SYSTEM",
		},
		UpstreamServices: []UpstreamService{
			{
				Name:                  "openai",
				BaseURL:               "https://a.example.com/v1",
				APIKey:                "key-a",
				IsDefault:             true,
				Models:                []string{"gpt-4o"},
				ClientKeys:            []string{"client-a"},
				PromptInjectionTarget: "instructions",
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected config to validate, got %v", err)
	}
	if cfg.Features.PromptInjectionTarget != PromptInjectionTargetSystem {
		t.Fatalf("expected normalized feature prompt injection target, got %q", cfg.Features.PromptInjectionTarget)
	}
	if cfg.UpstreamServices[0].PromptInjectionTarget != PromptInjectionTargetInstructions {
		t.Fatalf("expected normalized upstream prompt injection target, got %q", cfg.UpstreamServices[0].PromptInjectionTarget)
	}
}

func TestValidateAcceptsLastUserPromptInjectionTarget(t *testing.T) {
	cfg := &AppConfig{
		Server: ServerConfig{
			Port:    8000,
			Host:    "0.0.0.0",
			Timeout: 180,
		},
		Features: FeaturesConfig{
			LogLevel:              "INFO",
			PromptTemplate:        "{tool_catalog}\n{protocol_rules}\n{output_rules}",
			PromptInjectionTarget: "LAST_USER_MESSAGE",
		},
		UpstreamServices: []UpstreamService{
			{
				Name:                  "openai",
				BaseURL:               "https://a.example.com/v1",
				APIKey:                "key-a",
				IsDefault:             true,
				Models:                []string{"gpt-4o"},
				ClientKeys:            []string{"client-a"},
				PromptInjectionTarget: "last_user_message",
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected config to validate, got %v", err)
	}
	if cfg.Features.PromptInjectionTarget != PromptInjectionTargetLastUser {
		t.Fatalf("expected normalized feature prompt injection target, got %q", cfg.Features.PromptInjectionTarget)
	}
	if cfg.UpstreamServices[0].PromptInjectionTarget != PromptInjectionTargetLastUser {
		t.Fatalf("expected normalized upstream prompt injection target, got %q", cfg.UpstreamServices[0].PromptInjectionTarget)
	}
}

func TestValidateAcceptsMarkdownBlockSoftToolProtocol(t *testing.T) {
	cfg := &AppConfig{
		Server: ServerConfig{
			Port:    8000,
			Host:    "0.0.0.0",
			Timeout: 180,
		},
		Features: FeaturesConfig{
			LogLevel:         "INFO",
			PromptTemplate:   "{tool_catalog}\n{protocol_rules}\n{output_rules}",
			SoftToolProtocol: "MARKDOWN_BLOCK",
		},
		UpstreamServices: []UpstreamService{
			{
				Name:             "openai",
				BaseURL:          "https://a.example.com/v1",
				APIKey:           "key-a",
				IsDefault:        true,
				Models:           []string{"gpt-4o"},
				ClientKeys:       []string{"client-a"},
				SoftToolProtocol: "markdown_block",
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected config to validate, got %v", err)
	}
	if cfg.Features.SoftToolProtocol != SoftToolProtocolMarkdownBlock {
		t.Fatalf("expected feature soft tool protocol to normalize to markdown_block, got %q", cfg.Features.SoftToolProtocol)
	}
	if cfg.UpstreamServices[0].SoftToolProtocol != SoftToolProtocolMarkdownBlock {
		t.Fatalf("expected upstream soft tool protocol to normalize to markdown_block, got %q", cfg.UpstreamServices[0].SoftToolProtocol)
	}
}

func TestValidateRejectsPromptTemplateWithoutToolCatalogPlaceholder(t *testing.T) {
	cfg := &AppConfig{
		Server: ServerConfig{
			Port:    8000,
			Host:    "0.0.0.0",
			Timeout: 180,
		},
		Features: FeaturesConfig{
			LogLevel:       "INFO",
			PromptTemplate: "{protocol_rules}\n{output_rules}",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected prompt template without tool catalog placeholder to be rejected")
	}
}

func TestValidateRejectsLegacyPromptTemplatePlaceholder(t *testing.T) {
	cfg := &AppConfig{
		Server: ServerConfig{
			Port:    8000,
			Host:    "0.0.0.0",
			Timeout: 180,
		},
		Features: FeaturesConfig{
			LogLevel:       "INFO",
			PromptTemplate: "{tools_list}\n{protocol_rules}",
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected legacy tools_list placeholder to be rejected")
	}
}

func TestValidateAcceptsSoftToolPromptProfilesAndBindings(t *testing.T) {
	cfg := &AppConfig{
		Server: ServerConfig{
			Port:    8000,
			Host:    "0.0.0.0",
			Timeout: 180,
		},
		Features: FeaturesConfig{
			LogLevel:                       "INFO",
			PromptTemplate:                 "{tool_catalog}\n{protocol_rules}\n{output_rules}",
			DefaultSoftToolPromptProfileID: "markdown-weak",
		},
		SoftToolPromptProfiles: []SoftToolPromptProfile{
			{
				ID:       "markdown-weak",
				Name:     "Weak Markdown",
				Protocol: "MARKDOWN_BLOCK",
				Template: "{tool_catalog}\n{protocol_rules}\n{single_call_example}",
				Enabled:  true,
			},
			{
				ID:       "json-strong",
				Name:     "Strong JSON",
				Protocol: "sentinel_json",
				Enabled:  true,
			},
		},
		UpstreamServices: []UpstreamService{
			{
				Name:                    "openai",
				BaseURL:                 "https://a.example.com/v1",
				APIKey:                  "key-a",
				IsDefault:               true,
				Models:                  []string{"gpt-4o"},
				ClientKeys:              []string{"client-a"},
				SoftToolPromptProfileID: "json-strong",
			},
		},
	}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected config to validate, got %v", err)
	}
	if cfg.SoftToolPromptProfiles[0].Protocol != SoftToolProtocolMarkdownBlock {
		t.Fatalf("expected profile protocol to normalize to markdown_block, got %q", cfg.SoftToolPromptProfiles[0].Protocol)
	}
	if cfg.UpstreamServices[0].SoftToolPromptProfileID != "json-strong" {
		t.Fatalf("expected upstream profile binding to persist, got %q", cfg.UpstreamServices[0].SoftToolPromptProfileID)
	}
}

func TestValidateRejectsDisabledDefaultSoftToolPromptProfile(t *testing.T) {
	cfg := &AppConfig{
		Server: ServerConfig{
			Port:    8000,
			Host:    "0.0.0.0",
			Timeout: 180,
		},
		Features: FeaturesConfig{
			LogLevel:                       "INFO",
			PromptTemplate:                 "{tool_catalog}\n{protocol_rules}\n{output_rules}",
			DefaultSoftToolPromptProfileID: "disabled-profile",
		},
		SoftToolPromptProfiles: []SoftToolPromptProfile{{
			ID:       "disabled-profile",
			Name:     "Disabled Profile",
			Protocol: "xml",
			Template: "{tool_catalog}\n{protocol_rules}\n{single_call_example}",
			Enabled:  false,
		}},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected disabled default prompt profile to be rejected")
	}
}

func TestValidateRejectsPromptProfileBindingToMissingProfile(t *testing.T) {
	cfg := &AppConfig{
		Server: ServerConfig{
			Port:    8000,
			Host:    "0.0.0.0",
			Timeout: 180,
		},
		Features: FeaturesConfig{
			LogLevel:       "INFO",
			PromptTemplate: "{tool_catalog}\n{protocol_rules}\n{output_rules}",
		},
		UpstreamServices: []UpstreamService{
			{
				Name:                    "openai",
				BaseURL:                 "https://a.example.com/v1",
				APIKey:                  "key-a",
				IsDefault:               true,
				Models:                  []string{"gpt-4o"},
				ClientKeys:              []string{"client-a"},
				SoftToolPromptProfileID: "missing-profile",
			},
		},
	}

	if err := cfg.Validate(); err == nil {
		t.Fatalf("expected missing upstream soft tool prompt profile binding to be rejected")
	}
}
