package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallPlugins(t *testing.T) {
	sourceDir := t.TempDir()
	destinationDir := filepath.Join(t.TempDir(), "plugins")
	files := map[string]string{
		"cpa-copilot-provider.so": "copilot",
		"cpa-cursor-provider.so":  "cursor",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(sourceDir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := installPlugins(sourceDir, destinationDir); err != nil {
		t.Fatal(err)
	}
	for name, want := range files {
		path := filepath.Join(destinationDir, name)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != want {
			t.Fatalf("%s content = %q, want %q", name, content, want)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Fatalf("%s mode = %o", name, info.Mode().Perm())
		}
	}
}

func TestPrepareCopilotCache(t *testing.T) {
	root := t.TempDir()
	binary := filepath.Join(root, "fake-copilot")
	marker := filepath.Join(root, "marker")
	script := "#!/bin/sh\nprintf '%s\\n%s\\n%s\\n' \"$1\" \"$COPILOT_CACHE_HOME\" \"$COPILOT_AUTO_UPDATE\" > \"$TEST_MARKER\"\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_MARKER", marker)
	cacheDir := filepath.Join(root, "shared-cache")
	if err := prepareCopilotCache(binary, cacheDir); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(content)), "\n")
	if len(lines) != 3 || lines[0] != "version" || lines[1] != cacheDir || lines[2] != "false" {
		t.Fatalf("Copilot cache preparation environment = %q", content)
	}
}
