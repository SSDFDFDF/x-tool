package proxy

import (
	"strings"

	"x-tool/internal/config"
	"x-tool/internal/protocol"
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
		if target == config.PromptInjectionTargetLastUser {
			return promptInjectionSettings{Target: config.PromptInjectionTargetLastUser}
		}
		if target == config.PromptInjectionTargetMessage || (target == "" && isConversationRole(role)) {
			return promptInjectionSettings{
				Target: config.PromptInjectionTargetMessage,
				Role:   normalizeResponsesMessageRole(role),
			}
		}
		return promptInjectionSettings{Target: config.PromptInjectionTargetInstructions}

	case config.UpstreamProtocolAnthropic:
		if target == config.PromptInjectionTargetLastUser {
			return promptInjectionSettings{Target: config.PromptInjectionTargetLastUser}
		}
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
		if target == config.PromptInjectionTargetLastUser {
			return promptInjectionSettings{Target: config.PromptInjectionTargetLastUser}
		}
		// OpenAI Chat: defaults to injecting an extra message.
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

func lastOriginalUserMessageIndex(messages []map[string]any) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if role, _ := messages[i]["role"].(string); role == "user" {
			return i
		}
	}
	return -1
}

func prependPromptText(prompt, existing string) string {
	if strings.TrimSpace(existing) == "" {
		return prompt
	}
	return prompt + "\n\n" + existing
}

func prependOpenAIPromptPart(content any, prompt string) any {
	switch value := content.(type) {
	case nil:
		return prompt
	case string:
		return prependPromptText(prompt, value)
	case []any:
		promptPart := map[string]any{
			"type": "text",
			"text": prompt,
		}
		if len(value) > 0 {
			promptPart["text"] = prompt + "\n\n"
		}
		return append([]any{promptPart}, value...)
	default:
		return prependPromptText(prompt, stringifyProxyValue(value))
	}
}

func prependAnthropicPromptPart(content any, prompt string) any {
	switch value := content.(type) {
	case nil:
		return prompt
	case string:
		return prependPromptText(prompt, value)
	case []any:
		promptPart := map[string]any{
			"type": "text",
			"text": prompt,
		}
		if len(value) > 0 {
			promptPart["text"] = prompt + "\n\n"
		}
		return append([]any{promptPart}, value...)
	default:
		return prependPromptText(prompt, stringifyProxyValue(value))
	}
}

func prependResponsesPromptPart(content any, prompt string) any {
	promptText := prompt

	switch value := content.(type) {
	case nil:
		return []map[string]any{{
			"type": "input_text",
			"text": promptText,
		}}
	case string:
		return []map[string]any{{
			"type": "input_text",
			"text": prependPromptText(prompt, value),
		}}
	case []any:
		if len(value) > 0 {
			promptText = prompt + "\n\n"
		}
		return append([]any{map[string]any{
			"type": "input_text",
			"text": promptText,
		}}, value...)
	case []map[string]any:
		if len(value) > 0 {
			promptText = prompt + "\n\n"
		}
		return append([]map[string]any{{
			"type": "input_text",
			"text": promptText,
		}}, value...)
	default:
		return []map[string]any{{
			"type": "input_text",
			"text": prependPromptText(prompt, stringifyProxyValue(value)),
		}}
	}
}

func (a *App) injectPromptIntoLatestChatUserMessage(messages, original []map[string]any, prompt string) []map[string]any {
	index := lastOriginalUserMessageIndex(original)
	if index >= 0 && index < len(messages) {
		messages[index]["content"] = prependOpenAIPromptPart(messages[index]["content"], prompt)
		return messages
	}

	a.logger.Warn("prompt injection target last_user_message had no user message; appending fallback user message", "protocol", "chat.completions")
	return append(messages, map[string]any{
		"role":    "user",
		"content": prompt,
	})
}

func (a *App) injectPromptIntoLatestAnthropicUserMessage(messages, original []map[string]any, prompt string) []map[string]any {
	index := lastOriginalUserMessageIndex(original)
	if index >= 0 && index < len(messages) {
		messages[index]["content"] = prependAnthropicPromptPart(messages[index]["content"], prompt)
		return messages
	}

	a.logger.Warn("prompt injection target last_user_message had no user message; appending fallback user message", "protocol", "anthropic.messages")
	return append(messages, map[string]any{
		"role":    "user",
		"content": prompt,
	})
}

func (a *App) injectPromptIntoLatestResponsesUserInput(input any, prompt string) any {
	switch value := input.(type) {
	case nil:
		return []any{responsesPromptMessage(prompt)}
	case string:
		return prependPromptText(prompt, value)
	case []any:
		for i := len(value) - 1; i >= 0; i-- {
			item, _ := value[i].(map[string]any)
			if itemType, _ := item["type"].(string); itemType != "message" {
				continue
			}
			if role, _ := item["role"].(string); role != "user" {
				continue
			}
			cloned := protocol.CloneMap(item)
			cloned["content"] = prependResponsesPromptPart(cloned["content"], prompt)
			out := append([]any(nil), value...)
			out[i] = cloned
			return out
		}
		a.logger.Warn("prompt injection target last_user_message had no user message; appending fallback user message", "protocol", "responses")
		return append(append([]any(nil), value...), responsesPromptMessage(prompt))
	default:
		a.logger.Warn("prompt injection target last_user_message found non-message responses input; appending fallback user message", "protocol", "responses")
		return []any{value, responsesPromptMessage(prompt)}
	}
}

func responsesPromptMessage(prompt string) map[string]any {
	return map[string]any{
		"type": "message",
		"role": "user",
		"content": []map[string]any{{
			"type": "input_text",
			"text": prompt,
		}},
	}
}
