package bridge

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServerHealthAndAuthorization(t *testing.T) {
	server := newTestServer(t, "test-secret")

	health := httptest.NewRecorder()
	server.Handler().ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d", health.Code)
	}

	unauthorized := httptest.NewRecorder()
	server.Handler().ServeHTTP(
		unauthorized,
		httptest.NewRequest(http.MethodPost, "/v1/oauth/start", bytes.NewBufferString(`{"provider":"copilot"}`)),
	)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", unauthorized.Code)
	}
}

func TestServerCopilotDeviceFlowContract(t *testing.T) {
	server := newTestServer(t, "")
	response := postJSON(t, server, "/v1/oauth/start", map[string]any{"provider": "copilot"})
	if response.Code != http.StatusOK {
		t.Fatalf("start status = %d: %s", response.Code, response.Body.String())
	}

	var started struct {
		URL      string `json:"url"`
		State    string `json:"state"`
		Metadata struct {
			UserCode        string `json:"user_code"`
			VerificationURI string `json:"verification_uri"`
		} `json:"metadata"`
	}
	decodeResponse(t, response, &started)
	if started.URL != "https://github.com/login/device?user_code=ABCD-EFGH" {
		t.Fatalf("authorization URL = %q", started.URL)
	}
	if started.State == "" || started.Metadata.UserCode != "ABCD-EFGH" {
		t.Fatalf("start response = %+v", started)
	}

	var polled struct {
		Status string     `json:"status"`
		Auth   AuthRecord `json:"auth"`
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		response = postJSON(t, server, "/v1/oauth/poll", map[string]any{
			"provider": "copilot",
			"state":    started.State,
		})
		decodeResponse(t, response, &polled)
		if polled.Status != "pending" || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if polled.Status != "success" {
		t.Fatalf("poll status = %q", polled.Status)
	}
	if polled.Auth.Type != "subscription-bridge" || polled.Auth.Upstream != "copilot" {
		t.Fatalf("auth record = %+v", polled.Auth)
	}
}

func TestServerRejectsInvalidHandle(t *testing.T) {
	server := newTestServer(t, "")
	response := postJSON(t, server, "/v1/models", map[string]any{
		"provider": "cursor",
		"handle":   "../escape",
	})
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
}

func newTestServer(t *testing.T, secret string) *Server {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "fake-cli")
	script := "#!/bin/sh\nprintf 'Open https://github.com/login/device and enter ABCD-EFGH\\n'\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(Options{
		DataDir:       t.TempDir(),
		CopilotBinary: binary,
		CursorBinary:  binary,
		CursorScript:  "",
		Secret:        secret,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server
}

func postJSON(t *testing.T, server *Server, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	content, err := io.ReadAll(response.Result().Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(content, target); err != nil {
		t.Fatalf("decode %q: %v", content, err)
	}
}
