package bridge

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func promptFromPayload(payload map[string]any) (string, error) {
	messages, _ := payload["messages"].([]any)
	parts := make([]string, 0, len(messages)+1)
	for _, rawMessage := range messages {
		message, ok := rawMessage.(map[string]any)
		if !ok {
			continue
		}
		role := strings.ToUpper(stringValue(message["role"], "user"))
		content := flattenContent(message["content"])
		if content != "" {
			parts = append(parts, role+":\n"+content)
		}
	}
	if len(parts) == 0 {
		return "", errors.New("messages must contain at least one text message")
	}
	parts = append(parts, "ASSISTANT:")
	return strings.Join(parts, "\n\n"), nil
}

func flattenContent(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []any:
		texts := make([]string, 0, len(value))
		for _, item := range value {
			switch typed := item.(type) {
			case string:
				texts = append(texts, typed)
			case map[string]any:
				if text, ok := typed["text"].(string); ok {
					texts = append(texts, text)
				}
			}
		}
		return strings.Join(texts, "\n")
	default:
		return ""
	}
}

func completionPayload(model, content string) map[string]any {
	return map[string]any{
		"id":      "chatcmpl-" + randomID(),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       map[string]any{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}},
		"usage": map[string]any{
			"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0,
		},
	}
}

func streamChunks(model, content string) ([]string, error) {
	completionID := "chatcmpl-" + randomID()
	created := time.Now().Unix()
	chunks := []map[string]any{
		{
			"id": completionID, "object": "chat.completion.chunk", "created": created, "model": model,
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil,
			}},
		},
		{
			"id": completionID, "object": "chat.completion.chunk", "created": created, "model": model,
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{"content": content}, "finish_reason": nil,
			}},
		},
		{
			"id": completionID, "object": "chat.completion.chunk", "created": created, "model": model,
			"choices": []any{map[string]any{
				"index": 0, "delta": map[string]any{}, "finish_reason": "stop",
			}},
		},
	}
	result := make([]string, 0, 4)
	for _, chunk := range chunks {
		encoded, err := json.Marshal(chunk)
		if err != nil {
			return nil, fmt.Errorf("encode stream chunk: %w", err)
		}
		result = append(result, "data: "+string(encoded)+"\n\n")
	}
	return append(result, "data: [DONE]\n\n"), nil
}

func randomID() string {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}

func stringValue(value any, fallback string) string {
	if text, ok := value.(string); ok {
		return text
	}
	return fallback
}
