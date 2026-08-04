package provider

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

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

	raw, err = Handle(KindCopilot, pluginabi.MethodManagementRegister, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	var management managementRegistration
	if err := json.Unmarshal(response.Result, &management); err != nil {
		t.Fatal(err)
	}
	var hasQuota, hasDevice bool
	for _, resource := range management.Resources {
		hasQuota = hasQuota || resource.Path == "/quota"
		hasDevice = hasDevice || resource.Path == "/device"
	}
	if !hasQuota || !hasDevice {
		t.Fatalf("unexpected management resources: %+v", management.Resources)
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

func TestParseAuthAcceptsCPANormalizedProviderType(t *testing.T) {
	storage, _ := json.Marshal(storedAuth{Type: providerID(KindCopilot), Upstream: KindCopilot, Handle: "abc123"})
	request, _ := json.Marshal(pluginapi.AuthParseRequest{RawJSON: storage})

	raw, err := Handle(KindCopilot, pluginabi.MethodAuthParse, request)
	if err != nil {
		t.Fatal(err)
	}
	var response envelope
	_ = json.Unmarshal(raw, &response)
	var parsed pluginapi.AuthParseResponse
	_ = json.Unmarshal(response.Result, &parsed)
	if !parsed.Handled || parsed.Auth.Attributes["handle"] != "abc123" {
		t.Fatalf("unexpected parsed auth: %+v", parsed)
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

func TestDeviceFlowResourceUsesOpaqueState(t *testing.T) {
	state := "opaque state/value"
	t.Setenv("CPA_COPILOT_CURSOR_PUBLIC_BASE_URL", "https://cpa.example.com")
	if !rememberDeviceFlow(KindCopilot, state, map[string]any{
		"verification_uri": "https://github.com/login/device",
		"user_code":        "ABCD-EFGH",
	}, time.Now().Add(time.Minute)) {
		t.Fatal("expected device flow to be remembered")
	}
	t.Cleanup(func() { forgetDeviceFlow(KindCopilot, state) })

	authorizationURL := deviceFlowURL(KindCopilot, state)
	if strings.Contains(authorizationURL, "ABCD-EFGH") ||
		authorizationURL != "https://cpa.example.com/v0/resource/plugins/cpa-copilot-provider/device?state=opaque+state%2Fvalue" {
		t.Fatalf("device flow URL = %q", authorizationURL)
	}

	request, _ := json.Marshal(pluginapi.ManagementRequest{
		Path:    "/v0/resource/plugins/cpa-copilot-provider/device",
		Headers: http.Header{"Accept-Language": []string{"zh-CN"}},
		Query:   url.Values{"state": []string{state}},
	})
	raw, err := Handle(KindCopilot, pluginabi.MethodManagementHandle, request)
	if err != nil {
		t.Fatal(err)
	}
	var response envelope
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	var managementResponse pluginapi.ManagementResponse
	if err := json.Unmarshal(response.Result, &managementResponse); err != nil {
		t.Fatal(err)
	}
	body := string(managementResponse.Body)
	if managementResponse.StatusCode != http.StatusOK ||
		!strings.Contains(body, "ABCD-EFGH") ||
		!strings.Contains(body, "复制设备码") ||
		!strings.Contains(body, "https://github.com/login/device") {
		t.Fatalf("unexpected device flow response: status=%d body=%s", managementResponse.StatusCode, body)
	}
}

func TestDeviceFlowResourceRejectsExpiredState(t *testing.T) {
	state := "expired"
	if !rememberDeviceFlow(KindCopilot, state, map[string]any{
		"verification_uri": "https://github.com/login/device",
		"user_code":        "ABCD-EFGH",
	}, time.Now().Add(-time.Second)) {
		t.Fatal("expected device flow to be remembered")
	}

	request, _ := json.Marshal(pluginapi.ManagementRequest{
		Path:  "/v0/resource/plugins/cpa-copilot-provider/device",
		Query: url.Values{"state": []string{state}},
	})
	raw, err := Handle(KindCopilot, pluginabi.MethodManagementHandle, request)
	if err != nil {
		t.Fatal(err)
	}
	var response envelope
	if err := json.Unmarshal(raw, &response); err != nil {
		t.Fatal(err)
	}
	var managementResponse pluginapi.ManagementResponse
	if err := json.Unmarshal(response.Result, &managementResponse); err != nil {
		t.Fatal(err)
	}
	if managementResponse.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", managementResponse.StatusCode)
	}
}
