package protocol

import (
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kaptinlin/jsonrepair"
)

var ErrMissingAuthorization = errors.New("missing authorization header")

func newID(prefix string) string {
	buf := make([]byte, 12)
	if _, err := cryptorand.Read(buf); err != nil {
		return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s%x", prefix, buf)
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func UnmarshalJSONWithRepair(payload string, target any) bool {
	if err := json.Unmarshal([]byte(payload), target); err == nil {
		return true
	}

	repaired, err := jsonrepair.JSONRepair(payload)
	if err != nil {
		return false
	}
	if strings.TrimSpace(repaired) == "" {
		return false
	}
	return json.Unmarshal([]byte(repaired), target) == nil
}

func nilIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func finishReasonToStopReason(finishReason string) string {
	switch finishReason {
	case "tool_calls":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return "end_turn"
	}
}
