package main

import (
	"os"
	"path/filepath"
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
