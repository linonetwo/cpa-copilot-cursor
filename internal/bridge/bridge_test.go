package bridge

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestCredentialStoreRoundTrip(t *testing.T) {
	store, err := NewCredentialStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handle, err := store.NewHandle()
	if err != nil {
		t.Fatal(err)
	}
	record, err := store.SaveRecord("copilot", handle, "Primary", "")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadRecord("copilot", handle)
	if err != nil {
		t.Fatal(err)
	}
	if loaded != record {
		t.Fatalf("loaded record = %+v, want %+v", loaded, record)
	}
	accountDir, _ := store.AccountDir("copilot", handle)
	content, _ := os.ReadFile(filepath.Join(accountDir, "auth.json"))
	if strings.Contains(strings.ToLower(string(content)), "token") {
		t.Fatal("auth record contains token data")
	}
}

func TestCredentialStoreRejectsTraversal(t *testing.T) {
	store, _ := NewCredentialStore(t.TempDir())
	if _, err := store.AccountDir("cursor", "../escape"); err == nil {
		t.Fatal("expected invalid handle error")
	}
}

func TestDeviceFlowURLAndMetadata(t *testing.T) {
	target := authorizationURL("copilot", "https://github.com/login/device", "ABCD-EFGH")
	if target != "https://github.com/login/device?user_code=ABCD-EFGH" {
		t.Fatalf("authorization URL = %q", target)
	}
	metadata := loginMetadata("https://github.com/login/device", "ABCD-EFGH", "Enter the code")
	if metadata["user_code"] != "ABCD-EFGH" {
		t.Fatalf("metadata = %#v", metadata)
	}
}

func TestLoginFlowAcceptsPlaintextCredentialStorage(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "fake-copilot")
	script := "#!/bin/sh\n" +
		"printf 'Open https://github.com/login/device and enter ABCD-EFGH\\n'\n" +
		"printf 'System keychain unavailable. Store token in plaintext config file? (y/N) '\n" +
		"read answer\n" +
		"[ \"$answer\" = y ] || exit 1\n" +
		"printf 'Signed in successfully\\n'\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	flow, err := startLoginFlow("copilot", "test-handle", exec.Command(binary))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-flow.done:
	case <-time.After(2 * time.Second):
		flow.stop()
		t.Fatal("login flow did not answer plaintext credential prompt")
	}
	_, _, recent, finished, waitErr := flow.snapshot()
	if !finished || waitErr != nil {
		t.Fatalf("login flow finished=%v waitErr=%v output=%q", finished, waitErr, recent)
	}
	if !strings.Contains(recent, "Signed in successfully") {
		t.Fatalf("login output = %q", recent)
	}
}

func TestCursorNodeArguments(t *testing.T) {
	runtimes := NewRuntimes(nil, "", "/opt/cursor-agent/node", "/opt/cursor-agent/index.js")
	got := runtimes.cursorArguments("login")
	want := []string{"--use-system-ca", "/opt/cursor-agent/index.js", "login"}
	if !slices.Equal(got, want) {
		t.Fatalf("Cursor arguments = %#v, want %#v", got, want)
	}
}

func TestPromptAndOpenAIShapes(t *testing.T) {
	prompt, err := promptFromPayload(map[string]any{
		"messages": []any{
			map[string]any{"role": "system", "content": "Be concise."},
			map[string]any{"role": "user", "content": []any{
				map[string]any{"type": "text", "text": "Hello"},
				map[string]any{"type": "text", "text": "World"},
			}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "SYSTEM:\nBe concise.") || !strings.HasSuffix(prompt, "ASSISTANT:") {
		t.Fatalf("prompt = %q", prompt)
	}
	payload := completionPayload("model", "answer")
	encoded, _ := json.Marshal(payload)
	if !strings.Contains(string(encoded), `"content":"answer"`) {
		t.Fatalf("completion = %s", encoded)
	}
	chunks, err := streamChunks("model", "answer")
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 4 || chunks[3] != "data: [DONE]\n\n" {
		t.Fatalf("chunks = %#v", chunks)
	}
}
