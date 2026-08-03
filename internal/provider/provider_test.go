package provider

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestRegistrationExposesNativeOAuthAndQuotaResource(t *testing.T) {
	raw, err := Handle(KindCopilot, pluginabi.MethodPluginRegister, nil)
	if err != nil {
		t.Fatal(err)
	}
	var response envelope
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	var registration registration
	if err := json.Unmarshal(response.Result, &registration); err != nil {
		t.Fatal(err)
	}
	if !registration.Capabilities.AuthProvider || !registration.Capabilities.ManagementAPI {
		t.Fatalf("unexpected capabilities: %+v", registration.Capabilities)
	}
	if registration.Capabilities.ExecutorModelScope != pluginapi.ExecutorModelScopeOAuth {
		t.Fatalf("executor scope = %q", registration.Capabilities.ExecutorModelScope)
	}
}

func TestParseAuthRecognizesOnlyMatchingProvider(t *testing.T) {
	storage, _ := json.Marshal(storedAuth{Type: "copilot-cursor", Upstream: KindCursor, Handle: "abc123"})
	request, _ := json.Marshal(pluginapi.AuthParseRequest{RawJSON: storage})

	raw, err := Handle(KindCursor, pluginabi.MethodAuthParse, request)
	if err != nil {
		t.Fatal(err)
	}
	var response envelope
	_ = json.Unmarshal(raw, &response)
	var parsed pluginapi.AuthParseResponse
	_ = json.Unmarshal(response.Result, &parsed)
	if !parsed.Handled || parsed.Auth.Provider != "cursor" || parsed.Auth.Attributes["handle"] != "abc123" {
		t.Fatalf("unexpected parsed auth: %+v", parsed)
	}

	raw, err = Handle(KindCopilot, pluginabi.MethodAuthParse, request)
	if err != nil {
		t.Fatal(err)
	}
	_ = json.Unmarshal(raw, &response)
	_ = json.Unmarshal(response.Result, &parsed)
	if parsed.Handled {
		t.Fatal("copilot provider accepted cursor auth")
	}
}

func TestBridgeEnvironmentDefaults(t *testing.T) {
	oldEndpoint := os.Getenv("CPA_COPILOT_CURSOR_ENDPOINT")
	oldTimeout := os.Getenv("CPA_COPILOT_CURSOR_TIMEOUT")
	t.Cleanup(func() {
		_ = os.Setenv("CPA_COPILOT_CURSOR_ENDPOINT", oldEndpoint)
		_ = os.Setenv("CPA_COPILOT_CURSOR_TIMEOUT", oldTimeout)
	})
	_ = os.Unsetenv("CPA_COPILOT_CURSOR_ENDPOINT")
	_ = os.Setenv("CPA_COPILOT_CURSOR_TIMEOUT", "invalid")
	if bridgeEndpoint() != defaultBridgeEndpoint {
		t.Fatalf("endpoint = %q", bridgeEndpoint())
	}
	if bridgeTimeout() <= 0 {
		t.Fatal("bridge timeout must be positive")
	}
}
