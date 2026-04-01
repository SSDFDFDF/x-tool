package config

import "strings"

const BuiltInSoftToolPromptProfileClaudeCodeNativeID = "claude-code-native"

const builtInClaudeCodeNativePromptTemplate = `You are operating inside a Claude Code style agent runtime. Stay native-first and action-first.

Prefer the runtime's built-in tools and agent flows over narrating what you plan to do.
- If the available tools already cover the task, use them instead of simulating work in prose.
- For non-trivial work, prefer planning before implementation and keep the plan concrete.
- For codebase research, prefer search/read tools before broad shell exploration.
- For external systems, prefer MCP or connected-resource tools when they are present in the tool catalog.
- If one real user choice blocks progress, ask a concise question instead of guessing.
- Be concise, include exact paths/commands/validation results when relevant, and avoid long preambles.

Tools:
{tool_catalog}

{protocol_rules}

Single call shape:
{single_call_example}

Multiple call shape:
{multi_call_example}`

func BuiltInSoftToolPromptProfiles() []SoftToolPromptProfile {
	return []SoftToolPromptProfile{
		{
			ID:          BuiltInSoftToolPromptProfileClaudeCodeNativeID,
			Name:        "Claude Code Native",
			Description: "Native-first prompt profile for Claude Code style sessions and agent runtimes.",
			Protocol:    "",
			Template:    builtInClaudeCodeNativePromptTemplate,
			Enabled:     true,
		},
	}
}

func EnsureBuiltInSoftToolPromptProfiles(cfg *AppConfig) {
	if cfg == nil {
		return
	}
	cfg.SoftToolPromptProfiles = MergeBuiltInSoftToolPromptProfiles(cfg.SoftToolPromptProfiles)
}

func MergeBuiltInSoftToolPromptProfiles(profiles []SoftToolPromptProfile) []SoftToolPromptProfile {
	merged := append([]SoftToolPromptProfile(nil), profiles...)
	seen := make(map[string]struct{}, len(merged))
	for _, profile := range merged {
		id := strings.TrimSpace(profile.ID)
		if id == "" {
			continue
		}
		seen[id] = struct{}{}
	}

	for _, profile := range BuiltInSoftToolPromptProfiles() {
		if _, ok := seen[profile.ID]; ok {
			continue
		}
		merged = append(merged, profile)
	}

	return merged
}
