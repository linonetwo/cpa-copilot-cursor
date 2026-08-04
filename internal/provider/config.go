package provider

import (
	"os"
	"strings"
	"time"
)

const defaultBridgeEndpoint = "http://127.0.0.1:8789"

func bridgeEndpoint() string {
	value := strings.TrimRight(strings.TrimSpace(os.Getenv("CPA_COPILOT_CURSOR_ENDPOINT")), "/")
	if value == "" {
		return defaultBridgeEndpoint
	}
	return value
}

func bridgeSecret() string {
	return strings.TrimSpace(os.Getenv("CPA_COPILOT_CURSOR_SECRET"))
}

func bridgeTimeout() time.Duration {
	value := strings.TrimSpace(os.Getenv("CPA_COPILOT_CURSOR_TIMEOUT"))
	if value == "" {
		return 3 * time.Minute
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 3 * time.Minute
	}
	return duration
}

func providerID(kind Kind) string {
	if kind == KindCursor {
		return "cursor"
	}
	return "copilot"
}

func pluginID(kind Kind) string {
	if kind == KindCursor {
		return "cpa-cursor-provider"
	}
	return "cpa-copilot-provider"
}

func providerName(kind Kind) string {
	if kind == KindCursor {
		return "Cursor Subscription"
	}
	return "GitHub Copilot Subscription"
}

func pluginName(kind Kind) string {
	if kind == KindCursor {
		return "Cursor 订阅"
	}
	return "GitHub Copilot 订阅"
}
