package protocol

func ToolCallSlice(value any) []map[string]any {
	return toolCallSlice(value)
}

func toolCallSlice(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		result := make([]map[string]any, 0, len(typed))
		for _, raw := range typed {
			item, ok := raw.(map[string]any)
			if ok {
				result = append(result, item)
			}
		}
		return result
	default:
		return nil
	}
}
