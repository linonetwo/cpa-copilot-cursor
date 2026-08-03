package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
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
