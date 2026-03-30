package proxy

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"

	"x-tool/internal/protocol"
)

const (
	maxToolInt64 = int64(^uint64(0) >> 1)
	minToolInt64 = -maxToolInt64 - 1
)

type toolValidationRule struct {
	required map[string]struct{}
	schema   map[string]any
}

func buildToolValidationRules(tools []protocol.Tool) map[string]toolValidationRule {
	rules := make(map[string]toolValidationRule, len(tools))
	for _, tool := range tools {
		schema := normalizedToolParameters(tool.Function.Parameters)
		rules[tool.Function.Name] = toolValidationRule{
			required: requiredParameterSet(schema),
			schema:   schema,
		}
	}
	return rules
}

func normalizeToolArgsBySchema(args map[string]any, schema map[string]any) map[string]any {
	if len(args) == 0 || len(schema) == 0 {
		return args
	}

	normalized, ok := normalizeToolValueWithSchema(args, schema)
	if !ok {
		return args
	}
	normalizedArgs, ok := normalized.(map[string]any)
	if !ok {
		return args
	}
	return normalizedArgs
}

func normalizeToolValueBySchema(value any, schema map[string]any) any {
	normalized, ok := normalizeToolValueWithSchema(value, schema)
	if !ok {
		return value
	}
	return normalized
}

func normalizeToolValueWithSchema(value any, schema map[string]any) (any, bool) {
	if len(schema) == 0 {
		return value, true
	}

	types := schemaCandidateTypes(schema)
	if len(types) == 0 {
		return value, true
	}

	for _, schemaType := range types {
		if normalized, ok := normalizeToolValueForType(value, schemaType, schema, false); ok {
			return normalized, true
		}
	}
	for _, schemaType := range types {
		if normalized, ok := normalizeToolValueForType(value, schemaType, schema, true); ok {
			return normalized, true
		}
	}

	return value, false
}

func schemaCandidateTypes(schema map[string]any) []string {
	switch raw := schema["type"].(type) {
	case string:
		value := strings.TrimSpace(raw)
		if value != "" {
			return []string{value}
		}
	case []any:
		return uniqueNonEmptySchemaTypes(raw)
	case []string:
		items := make([]any, 0, len(raw))
		for _, item := range raw {
			items = append(items, item)
		}
		return uniqueNonEmptySchemaTypes(items)
	}

	if _, ok := schema["properties"].(map[string]any); ok {
		return []string{"object"}
	}
	if _, ok := schema["items"].(map[string]any); ok {
		return []string{"array"}
	}
	return nil
}

func uniqueNonEmptySchemaTypes(values []any) []string {
	seen := map[string]struct{}{}
	types := make([]string, 0, len(values))
	for _, item := range values {
		value, _ := item.(string)
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		types = append(types, value)
	}
	return types
}

func normalizeToolValueForType(value any, schemaType string, schema map[string]any, allowCoercion bool) (any, bool) {
	switch schemaType {
	case "string":
		return normalizeToolStringValue(value, allowCoercion)
	case "integer":
		return normalizeToolIntegerValue(value, allowCoercion)
	case "number":
		return normalizeToolNumberValue(value, allowCoercion)
	case "boolean":
		return normalizeToolBooleanValue(value, allowCoercion)
	case "object":
		return normalizeToolObjectValue(value, schema, allowCoercion)
	case "array":
		return normalizeToolArrayValue(value, schema, allowCoercion)
	case "null":
		return normalizeToolNullValue(value, allowCoercion)
	default:
		return value, false
	}
}

func normalizeToolStringValue(value any, allowCoercion bool) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, true
	case bool:
		if !allowCoercion {
			return "", false
		}
		if typed {
			return "true", true
		}
		return "false", true
	case float64:
		if !allowCoercion {
			return "", false
		}
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case float32:
		if !allowCoercion {
			return "", false
		}
		return strconv.FormatFloat(float64(typed), 'f', -1, 32), true
	case int:
		if !allowCoercion {
			return "", false
		}
		return strconv.FormatInt(int64(typed), 10), true
	case int8:
		if !allowCoercion {
			return "", false
		}
		return strconv.FormatInt(int64(typed), 10), true
	case int16:
		if !allowCoercion {
			return "", false
		}
		return strconv.FormatInt(int64(typed), 10), true
	case int32:
		if !allowCoercion {
			return "", false
		}
		return strconv.FormatInt(int64(typed), 10), true
	case int64:
		if !allowCoercion {
			return "", false
		}
		return strconv.FormatInt(typed, 10), true
	case uint:
		if !allowCoercion {
			return "", false
		}
		return strconv.FormatUint(uint64(typed), 10), true
	case uint8:
		if !allowCoercion {
			return "", false
		}
		return strconv.FormatUint(uint64(typed), 10), true
	case uint16:
		if !allowCoercion {
			return "", false
		}
		return strconv.FormatUint(uint64(typed), 10), true
	case uint32:
		if !allowCoercion {
			return "", false
		}
		return strconv.FormatUint(uint64(typed), 10), true
	case uint64:
		if !allowCoercion {
			return "", false
		}
		return strconv.FormatUint(typed, 10), true
	default:
		return "", false
	}
}

func normalizeToolIntegerValue(value any, allowCoercion bool) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int8:
		return int64(typed), true
	case int16:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case uint:
		if uint64(typed) > uint64(maxToolInt64) {
			return 0, false
		}
		return int64(typed), true
	case uint8:
		return int64(typed), true
	case uint16:
		return int64(typed), true
	case uint32:
		return int64(typed), true
	case uint64:
		if typed > uint64(maxToolInt64) {
			return 0, false
		}
		return int64(typed), true
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || math.Trunc(typed) != typed || typed < float64(minToolInt64) || typed > float64(maxToolInt64) {
			return 0, false
		}
		return int64(typed), true
	case float32:
		value := float64(typed)
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Trunc(value) != value || value < float64(minToolInt64) || value > float64(maxToolInt64) {
			return 0, false
		}
		return int64(value), true
	case string:
		if !allowCoercion {
			return 0, false
		}
		return parseIntegerString(typed)
	default:
		return 0, false
	}
}

func parseIntegerString(value string) (int64, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, false
	}
	if parsed, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return parsed, true
	}
	if parsed, err := strconv.ParseUint(trimmed, 10, 64); err == nil {
		if parsed > uint64(maxToolInt64) {
			return 0, false
		}
		return int64(parsed), true
	}
	if parsed, err := strconv.ParseFloat(trimmed, 64); err == nil {
		if math.IsNaN(parsed) || math.IsInf(parsed, 0) || math.Trunc(parsed) != parsed || parsed < float64(minToolInt64) || parsed > float64(maxToolInt64) {
			return 0, false
		}
		return int64(parsed), true
	}
	return 0, false
}

func normalizeToolNumberValue(value any, allowCoercion bool) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return 0, false
		}
		return typed, true
	case float32:
		value := float64(typed)
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, false
		}
		return value, true
	case int:
		return float64(typed), true
	case int8:
		return float64(typed), true
	case int16:
		return float64(typed), true
	case int32:
		return float64(typed), true
	case int64:
		return float64(typed), true
	case uint:
		return float64(typed), true
	case uint8:
		return float64(typed), true
	case uint16:
		return float64(typed), true
	case uint32:
		return float64(typed), true
	case uint64:
		return float64(typed), true
	case string:
		if !allowCoercion {
			return 0, false
		}
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return 0, false
		}
		parsed, err := strconv.ParseFloat(trimmed, 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func normalizeToolBooleanValue(value any, allowCoercion bool) (bool, bool) {
	switch typed := value.(type) {
	case bool:
		return typed, true
	case string:
		if !allowCoercion {
			return false, false
		}
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return false, false
		}
		parsed, err := strconv.ParseBool(trimmed)
		if err != nil {
			return false, false
		}
		return parsed, true
	default:
		return false, false
	}
}

func normalizeToolObjectValue(value any, schema map[string]any, allowCoercion bool) (map[string]any, bool) {
	objectValue, ok := value.(map[string]any)
	if !ok && allowCoercion {
		if raw, ok := value.(string); ok {
			decoded, ok := decodeJSONObjectString(raw)
			if !ok {
				return nil, false
			}
			objectValue = decoded
			ok = true
		}
	}
	if !ok {
		return nil, false
	}

	properties, _ := schema["properties"].(map[string]any)
	additionalProperties, _ := schema["additionalProperties"].(map[string]any)
	if len(properties) == 0 && len(additionalProperties) == 0 {
		return objectValue, true
	}

	normalized := make(map[string]any, len(objectValue))
	for name, child := range objectValue {
		childSchema, _ := properties[name].(map[string]any)
		if len(childSchema) == 0 {
			childSchema = additionalProperties
		}
		normalized[name] = normalizeToolValueBySchema(child, childSchema)
	}
	return normalized, true
}

func decodeJSONObjectString(value string) (map[string]any, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, false
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil, false
	}
	return decoded, true
}

func normalizeToolArrayValue(value any, schema map[string]any, allowCoercion bool) ([]any, bool) {
	arrayValue, ok := value.([]any)
	if !ok && allowCoercion {
		if raw, ok := value.(string); ok {
			decoded, ok := decodeJSONArrayString(raw)
			if !ok {
				return nil, false
			}
			arrayValue = decoded
			ok = true
		}
	}
	if !ok {
		return nil, false
	}

	itemSchema, _ := schema["items"].(map[string]any)
	if len(itemSchema) == 0 {
		return arrayValue, true
	}

	normalized := make([]any, 0, len(arrayValue))
	for _, item := range arrayValue {
		normalized = append(normalized, normalizeToolValueBySchema(item, itemSchema))
	}
	return normalized, true
}

func decodeJSONArrayString(value string) ([]any, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, false
	}
	var decoded []any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
		return nil, false
	}
	return decoded, true
}

func normalizeToolNullValue(value any, allowCoercion bool) (any, bool) {
	if value == nil {
		return nil, true
	}
	if !allowCoercion {
		return nil, false
	}
	text, ok := value.(string)
	if !ok {
		return nil, false
	}
	if strings.EqualFold(strings.TrimSpace(text), "null") {
		return nil, true
	}
	return nil, false
}
