package main

import (
	"testing"

	"x-tool/internal/config"
)

func TestDefaultConfigDoesNotSeedUpstreams(t *testing.T) {
	t.Setenv("X_TOOL_DEFAULT_UPSTREAM_URL", "https://legacy.example.com/v1")
	t.Setenv("X_TOOL_DEFAULT_API_KEY", "legacy-key")
	cfg := defaultConfig(&config.ServerEnv{})

	if cfg == nil {
		t.Fatalf("expected default config")
	}
	if len(cfg.UpstreamServices) != 0 {
		t.Fatalf("expected no seeded upstream services, got %d", len(cfg.UpstreamServices))
	}
	if len(cfg.ClientKeys()) != 0 {
		t.Fatalf("expected no seeded client keys, got %#v", cfg.ClientKeys())
	}
	if cfg.Features.LogLevel != "INFO" {
		t.Fatalf("expected default log level, got %q", cfg.Features.LogLevel)
	}
	if cfg.Features.PromptTemplate != "" {
		t.Fatalf("expected empty default prompt template, got %q", cfg.Features.PromptTemplate)
	}
}
