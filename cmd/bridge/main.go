package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/linonetwo/cpa-copilot-cursor/internal/bridge"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--healthcheck" {
		if err := healthcheck(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "--install-plugins" {
		if err := installPlugins("/plugins", os.Args[2]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "--prepare-copilot-cache" {
		if err := prepareCopilotCache(
			envOrDefault("COPILOT_CLI_PATH", "/opt/copilot/copilot"),
			os.Args[2],
		); err != nil {
			log.Fatal(err)
		}
		return
	}
	server, err := bridge.NewServer(bridge.Options{
		DataDir:       envOrDefault("CPA_COPILOT_CURSOR_DATA", "/data"),
		CopilotBinary: envOrDefault("COPILOT_CLI_PATH", "/opt/copilot/copilot"),
		CursorBinary:  envOrDefault("CURSOR_AGENT_PATH", "/opt/cursor-agent/node"),
		CursorScript:  envOrDefault("CURSOR_AGENT_SCRIPT", "/opt/cursor-agent/index.js"),
		Secret:        os.Getenv("CPA_COPILOT_CURSOR_SECRET"),
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := server.ListenAndServe("127.0.0.1:8789"); err != nil {
		log.Fatal(err)
	}
}

func prepareCopilotCache(binary, cacheDir string) error {
	if strings.TrimSpace(cacheDir) == "" {
		return fmt.Errorf("Copilot cache directory is required")
	}
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return fmt.Errorf("create Copilot cache directory: %w", err)
	}
	bootstrapHome := filepath.Join(cacheDir, "bootstrap-home")
	if err := os.MkdirAll(bootstrapHome, 0o700); err != nil {
		return fmt.Errorf("create Copilot bootstrap home: %w", err)
	}
	command := exec.Command(binary, "version")
	command.Env = commandEnvironment(os.Environ(), map[string]string{
		"COPILOT_AUTO_UPDATE": "false",
		"COPILOT_CACHE_HOME":  cacheDir,
		"COPILOT_HOME":        filepath.Join(bootstrapHome, ".copilot"),
		"HOME":                bootstrapHome,
	})
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("prepare Copilot runtime cache: %s: %w", strings.TrimSpace(string(output)), err)
	}
	return nil
}

func commandEnvironment(environment []string, values map[string]string) []string {
	result := append([]string{}, environment...)
	for name, value := range values {
		prefix := name + "="
		filtered := result[:0]
		for _, entry := range result {
			if !strings.HasPrefix(entry, prefix) {
				filtered = append(filtered, entry)
			}
		}
		result = append(filtered, prefix+value)
	}
	return result
}

func installPlugins(sourceDir, destinationDir string) error {
	if err := os.MkdirAll(destinationDir, 0o700); err != nil {
		return fmt.Errorf("create plugin directory: %w", err)
	}
	for _, name := range []string{"cpa-copilot-provider.so", "cpa-cursor-provider.so"} {
		if err := copyPlugin(filepath.Join(sourceDir, name), filepath.Join(destinationDir, name)); err != nil {
			return err
		}
	}
	return nil
}

func copyPlugin(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open plugin %s: %w", filepath.Base(source), err)
	}
	defer input.Close()

	temporary := destination + ".tmp"
	output, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create plugin %s: %w", filepath.Base(destination), err)
	}
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		os.Remove(temporary)
		return fmt.Errorf("copy plugin %s: %w", filepath.Base(destination), err)
	}
	if err := output.Close(); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("close plugin %s: %w", filepath.Base(destination), err)
	}
	if err := os.Chmod(temporary, 0o755); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("set plugin mode %s: %w", filepath.Base(destination), err)
	}
	if err := os.Rename(temporary, destination); err != nil {
		os.Remove(temporary)
		return fmt.Errorf("replace plugin %s: %w", filepath.Base(destination), err)
	}
	return nil
}

func healthcheck() error {
	client := &http.Client{Timeout: 4 * time.Second}
	response, err := client.Get("http://127.0.0.1:8789/healthz")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("bridge healthcheck returned %s", response.Status)
	}
	return nil
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
