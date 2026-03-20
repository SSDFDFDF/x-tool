package proxy

import (
	"x-tool/internal/config"
)

type promptInjectionSettings struct {
	Target string
	Role   string
}

// resolvePromptInjection determines where and how the soft-tool prompt is
// injected into the upstream request based on protocol and user configuration.
//
// Configuration priority: upstream service > global features.
// Each protocol has a natural "system-level" location (the default):
//   - Responses  → instructions field
//   - Anthropic  → system field
//   - OpenAI     → system role message
//
// Setting role to user/assistant (without explicit target) implies message injection.
func (a *App) resolvePromptInjection(upstream config.UpstreamService) promptInjectionSettings {
	target := firstNonEmpty(upstream.PromptInjectionTarget, a.Config().Features.PromptInjectionTarget)
	role := firstNonEmpty(upstream.PromptInjectionRole, a.Config().Features.PromptInjectionRole)

	switch upstream.UpstreamProtocol {
	case config.UpstreamProtocolResponses:
		if target == config.PromptInjectionTargetMessage || (target == "" && isConversationRole(role)) {
			return promptInjectionSettings{
				Target: config.PromptInjectionTargetMessage,
				Role:   normalizeResponsesMessageRole(role),
			}
		}
		return promptInjectionSettings{Target: config.PromptInjectionTargetInstructions}

	case config.UpstreamProtocolAnthropic:
		if target == config.PromptInjectionTargetMessage || (target == "" && isConversationRole(role)) {
			if isConversationRole(role) {
				return promptInjectionSettings{
					Target: config.PromptInjectionTargetMessage,
					Role:   role,
				}
			}
		}
		return promptInjectionSettings{Target: config.PromptInjectionTargetSystem}

	default:
		// OpenAI Chat: always injected as a message.
		if role == "" {
			role = "system"
		}
		return promptInjectionSettings{Target: config.PromptInjectionTargetMessage, Role: role}
	}
}

// normalizeResponsesMessageRole maps generic roles to Responses API roles.
// Responses API uses "developer" where OpenAI uses "system".
func normalizeResponsesMessageRole(role string) string {
	switch role {
	case "user":
		return "user"
	case "assistant":
		return "assistant"
	default:
		return "developer"
	}
}

// isConversationRole returns true for roles that imply message-level injection
// (as opposed to system-level).
func isConversationRole(role string) bool {
	return role == "user" || role == "assistant"
}

