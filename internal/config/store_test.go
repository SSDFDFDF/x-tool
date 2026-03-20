package config

import (
	"path/filepath"
	"testing"

	"x-tool/internal/storage"
)

func TestSeedWithNoUpstreamsSkipsUpstreamInsertAndSeedsOtherDefaults(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "x-tool-seed.db")
	db, err := storage.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	store := NewConfigStore(db)
	defaults := &AppConfig{
		UpstreamServices: []UpstreamService{},
		Features: FeaturesConfig{
			EnableFunctionCalling:    true,
			LogLevel:                 "INFO",
			ConvertDeveloperToSystem: true,
			PromptTemplate:           "{tool_catalog}\n{trigger_signal}",
			PromptInjectionRole:      "system",
			PromptInjectionTarget:    PromptInjectionTargetSystem,
			KeyPassthrough:           false,
			ModelPassthrough:         false,
		},
	}

	if err := store.Seed(defaults); err != nil {
		t.Fatalf("seed defaults: %v", err)
	}

	upstreams, err := store.ListUpstreams()
	if err != nil {
		t.Fatalf("list upstreams: %v", err)
	}
	if len(upstreams) != 0 {
		t.Fatalf("expected no seeded upstreams, got %d", len(upstreams))
	}

	features, err := store.GetAllFeatures()
	if err != nil {
		t.Fatalf("list features: %v", err)
	}
	if got := features[featureEnableFunctionCalling]; got != "true" {
		t.Fatalf("expected %s=true, got %q", featureEnableFunctionCalling, got)
	}
	if got := features[featureLogLevel]; got != "INFO" {
		t.Fatalf("expected %s=INFO, got %q", featureLogLevel, got)
	}
}

func TestSaveAndLoadAppConfigPersistsUpstreamClientKeys(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "x-tool-config.db")
	db, err := storage.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})

	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	store := NewConfigStore(db)
	cfg := &AppConfig{
		UpstreamServices: []UpstreamService{
			{
				Name:                    "openai",
				BaseURL:                 "https://api.openai.com/v1",
				APIKey:                  "upstream-key",
				IsDefault:               true,
				Models:                  []string{"gpt-4o"},
				ClientKeys:              []string{"client-a", "client-b"},
				UpstreamProtocol:        UpstreamProtocolResponses,
				PromptInjectionTarget:   PromptInjectionTargetMessage,
				SoftToolPromptProfileID: "weak-markdown",
			},
		},
		SoftToolPromptProfiles: []SoftToolPromptProfile{
			{
				ID:          "weak-markdown",
				Name:        "Weak Markdown",
				Description: "Simple template for weak models",
				Protocol:    SoftToolProtocolMarkdownBlock,
				Template:    "{tool_catalog}\n{protocol_rules}\n{single_call_example}",
				Enabled:     true,
			},
		},
		Features: FeaturesConfig{
			EnableFunctionCalling:          true,
			LogLevel:                       "INFO",
			ConvertDeveloperToSystem:       true,
			PromptTemplate:                 "{tool_catalog}\n{trigger_signal}",
			DefaultSoftToolPromptProfileID: "weak-markdown",
			PromptInjectionRole:            "system",
			PromptInjectionTarget:          PromptInjectionTargetInstructions,
		},
	}

	if err := store.SaveAppConfig(cfg); err != nil {
		t.Fatalf("save app config: %v", err)
	}

	loaded, err := store.LoadAppConfig(nil)
	if err != nil {
		t.Fatalf("load app config: %v", err)
	}
	if len(loaded.UpstreamServices) != 1 {
		t.Fatalf("expected 1 upstream, got %d", len(loaded.UpstreamServices))
	}
	if got := loaded.UpstreamServices[0].ClientKeys; len(got) != 2 || got[0] != "client-a" || got[1] != "client-b" {
		t.Fatalf("expected persisted client keys, got %#v", got)
	}
	if loaded.UpstreamServices[0].UpstreamProtocol != UpstreamProtocolResponses {
		t.Fatalf("expected upstream protocol to persist, got %q", loaded.UpstreamServices[0].UpstreamProtocol)
	}
	if loaded.UpstreamServices[0].PromptInjectionTarget != PromptInjectionTargetMessage {
		t.Fatalf("expected upstream prompt injection target to persist, got %q", loaded.UpstreamServices[0].PromptInjectionTarget)
	}
	if loaded.UpstreamServices[0].SoftToolPromptProfileID != "weak-markdown" {
		t.Fatalf("expected upstream prompt profile id to persist, got %q", loaded.UpstreamServices[0].SoftToolPromptProfileID)
	}
	if loaded.Features.PromptInjectionTarget != PromptInjectionTargetInstructions {
		t.Fatalf("expected feature prompt injection target to persist, got %q", loaded.Features.PromptInjectionTarget)
	}
	if loaded.Features.DefaultSoftToolPromptProfileID != "weak-markdown" {
		t.Fatalf("expected default prompt profile id to persist, got %q", loaded.Features.DefaultSoftToolPromptProfileID)
	}
	if len(loaded.SoftToolPromptProfiles) != 1 {
		t.Fatalf("expected 1 prompt profile, got %d", len(loaded.SoftToolPromptProfiles))
	}
	if loaded.SoftToolPromptProfiles[0].Protocol != SoftToolProtocolMarkdownBlock {
		t.Fatalf("expected prompt profile protocol to persist, got %q", loaded.SoftToolPromptProfiles[0].Protocol)
	}
	if got := loaded.ClientKeys(); len(got) != 2 {
		t.Fatalf("expected unique client key list, got %#v", got)
	}
}
