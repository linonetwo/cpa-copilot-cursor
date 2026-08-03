package bridge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
)

type Runtimes struct {
	store         *CredentialStore
	copilotBinary string
	cursorBinary  string
	cursorScript  string

	mu     sync.Mutex
	logins map[string]*loginFlow
}

func NewRuntimes(store *CredentialStore, copilotBinary, cursorBinary, cursorScript string) *Runtimes {
	return &Runtimes{
		store: store, copilotBinary: copilotBinary, cursorBinary: cursorBinary, cursorScript: cursorScript,
		logins: make(map[string]*loginFlow),
	}
}

func (r *Runtimes) StartLogin(ctx context.Context, provider string) (string, string, map[string]any, error) {
	handle, err := r.store.NewHandle()
	if err != nil {
		return "", "", nil, err
	}
	state, err := r.store.NewHandle()
	if err != nil {
		return "", "", nil, err
	}
	home, err := r.store.HomeDir(provider, handle)
	if err != nil {
		return "", "", nil, err
	}
	command, err := r.loginCommand(provider, home)
	if err != nil {
		return "", "", nil, err
	}
	flow, err := startLoginFlow(provider, handle, command)
	if err != nil {
		return "", "", nil, fmt.Errorf("start %s login: %w", provider, err)
	}
	r.mu.Lock()
	r.logins[state] = flow
	r.mu.Unlock()
	go r.expireLogin(state, flow)

	timeout := time.NewTimer(30 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		url, userCode, recent, finished, waitErr := flow.snapshot()
		if url != "" {
			return authorizationURL(provider, url, userCode), state, loginMetadata(url, userCode, recent), nil
		}
		if finished {
			r.removeLogin(state)
			if waitErr == nil {
				waitErr = errors.New("login command exited")
			}
			return "", "", nil, fmt.Errorf("%s login did not produce an authorization URL: %s: %w", provider, recent, waitErr)
		}
		select {
		case <-ctx.Done():
			flow.stop()
			r.removeLogin(state)
			return "", "", nil, ctx.Err()
		case <-timeout.C:
			flow.stop()
			r.removeLogin(state)
			return "", "", nil, fmt.Errorf("%s login did not produce an authorization URL: %s", provider, recent)
		case <-ticker.C:
		}
	}
}

func (r *Runtimes) PollLogin(provider, state string) (string, string, *AuthRecord) {
	r.mu.Lock()
	flow := r.logins[state]
	r.mu.Unlock()
	if flow == nil || flow.provider != provider {
		return "error", "login state was not found or expired", nil
	}
	_, _, recent, finished, waitErr := flow.snapshot()
	if !finished {
		if recent == "" {
			recent = "waiting for browser authorization"
		}
		return "pending", recent, nil
	}
	r.removeLogin(state)
	if waitErr != nil {
		if recent == "" {
			recent = waitErr.Error()
		}
		return "error", recent, nil
	}
	record, err := r.store.SaveRecord(
		provider,
		flow.handle,
		strings.ToUpper(provider[:1])+provider[1:]+" subscription "+flow.handle[:6],
		"",
	)
	if err != nil {
		return "error", err.Error(), nil
	}
	return "success", "login completed", &record
}

func (r *Runtimes) ListModels(ctx context.Context, provider, handle string) ([]map[string]any, error) {
	if err := r.store.AssertAccount(provider, handle); err != nil {
		return nil, err
	}
	if provider == "cursor" {
		return r.cursorModels(ctx, handle)
	}
	client, err := r.copilotClient(provider, handle)
	if err != nil {
		return nil, err
	}
	if err := client.Start(ctx); err != nil {
		return nil, err
	}
	defer client.Stop()
	models, err := client.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]any, 0, len(models))
	for _, model := range models {
		contextLength := 0
		if model.Capabilities.Limits.MaxContextWindowTokens != nil {
			contextLength = *model.Capabilities.Limits.MaxContextWindowTokens
		}
		name := model.Name
		if name == "" {
			name = model.ID
		}
		result = append(result, map[string]any{
			"id": model.ID, "display_name": name, "description": "GitHub Copilot model",
			"context_length": contextLength, "max_completion_tokens": 0,
		})
	}
	return result, nil
}

func (r *Runtimes) Execute(ctx context.Context, provider, handle, model string, payload map[string]any, stream bool) (map[string]any, error) {
	if err := r.store.AssertAccount(provider, handle); err != nil {
		return nil, err
	}
	prompt, err := promptFromPayload(payload)
	if err != nil {
		return nil, err
	}
	var content string
	if provider == "copilot" {
		content, err = r.copilotExecute(ctx, handle, model, prompt)
	} else {
		content, err = r.cursorExecute(ctx, handle, model, prompt)
	}
	if err != nil {
		return nil, err
	}
	if stream {
		chunks, err := streamChunks(model, content)
		if err != nil {
			return nil, err
		}
		return map[string]any{"payload": map[string]any{}, "chunks": chunks}, nil
	}
	return map[string]any{"payload": completionPayload(model, content)}, nil
}

func (r *Runtimes) Quota(ctx context.Context, provider, handle string) (any, error) {
	if err := r.store.AssertAccount(provider, handle); err != nil {
		return nil, err
	}
	if provider == "cursor" {
		output, err := r.runCursor(ctx, handle, []string{"status"}, 30*time.Second)
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": strings.TrimSpace(output), "source": "cursor-agent status"}, nil
	}
	client, err := r.copilotClient(provider, handle)
	if err != nil {
		return nil, err
	}
	if err := client.Start(ctx); err != nil {
		return nil, err
	}
	defer client.Stop()
	return client.RPC.Account.GetQuota(ctx, &rpc.AccountGetQuotaRequest{})
}

func (r *Runtimes) loginCommand(provider, home string) (*exec.Cmd, error) {
	var command *exec.Cmd
	switch provider {
	case "copilot":
		command = exec.Command(r.copilotBinary, "login")
	case "cursor":
		command = exec.Command(r.cursorBinary, r.cursorArguments("login")...)
	default:
		return nil, errors.New("unsupported provider")
	}
	command.Env = accountEnvironment(provider, home)
	return command, nil
}

func (r *Runtimes) copilotClient(provider, handle string) (*copilot.Client, error) {
	home, err := r.store.HomeDir(provider, handle)
	if err != nil {
		return nil, err
	}
	useLoggedInUser := true
	baseDirectory := filepath.Join(home, ".copilot")
	if err := os.MkdirAll(baseDirectory, 0o700); err != nil {
		return nil, err
	}
	return copilot.NewClient(&copilot.ClientOptions{
		Connection:       copilot.StdioConnection{Path: r.copilotBinary, Env: accountEnvironment(provider, home)},
		WorkingDirectory: home,
		BaseDirectory:    baseDirectory,
		UseLoggedInUser:  &useLoggedInUser,
		Mode:             copilot.ModeEmpty,
	}), nil
}

func (r *Runtimes) copilotExecute(ctx context.Context, handle, model, prompt string) (string, error) {
	client, err := r.copilotClient("copilot", handle)
	if err != nil {
		return "", err
	}
	home, err := r.store.HomeDir("copilot", handle)
	if err != nil {
		return "", err
	}
	if err := client.Start(ctx); err != nil {
		return "", err
	}
	defer client.Stop()
	skipInstructions := true
	config := &copilot.SessionConfig{
		ClientName:             "cpa-copilot-cursor",
		Model:                  model,
		Tools:                  []copilot.Tool{},
		AvailableTools:         []string{},
		WorkingDirectory:       home,
		SkipCustomInstructions: &skipInstructions,
	}
	session, err := client.CreateSession(ctx, config)
	if err != nil {
		return "", err
	}
	defer session.Disconnect()
	callContext, cancel := context.WithTimeout(ctx, 170*time.Second)
	defer cancel()
	event, err := session.SendAndWait(callContext, copilot.MessageOptions{Prompt: prompt})
	if err != nil {
		return "", err
	}
	if event == nil {
		return "", nil
	}
	message, ok := event.Data.(*copilot.AssistantMessageData)
	if !ok {
		return "", fmt.Errorf("unexpected Copilot response type %T", event.Data)
	}
	return message.Content, nil
}

func (r *Runtimes) cursorModels(ctx context.Context, handle string) ([]map[string]any, error) {
	output, err := r.runCursor(ctx, handle, []string{"--list-models"}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	pattern := regexp.MustCompile(`^[A-Za-z0-9._:/-]+$`)
	models := make([]map[string]any, 0)
	for _, line := range strings.Split(output, "\n") {
		value := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "-* "))
		if value == "" || strings.Contains(strings.ToLower(value), "model") && !strings.Contains(value, ":") {
			continue
		}
		modelID := strings.Fields(value)[0]
		if pattern.MatchString(modelID) {
			models = append(models, map[string]any{"id": modelID, "display_name": value})
		}
	}
	if len(models) == 0 {
		models = append(models, map[string]any{"id": "auto", "display_name": "Auto"})
	}
	return models, nil
}

func (r *Runtimes) cursorExecute(ctx context.Context, handle, model, prompt string) (string, error) {
	workspace, err := os.MkdirTemp("", "cpa-cursor-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(workspace)
	args := []string{"--print", "--trust", "--mode", "ask", "--workspace", workspace, "--output-format", "text"}
	if model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, prompt)
	output, err := r.runCursor(ctx, handle, args, 170*time.Second)
	return strings.TrimSpace(output), err
}

func (r *Runtimes) runCursor(ctx context.Context, handle string, args []string, timeout time.Duration) (string, error) {
	home, err := r.store.HomeDir("cursor", handle)
	if err != nil {
		return "", err
	}
	callContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	command := exec.CommandContext(callContext, r.cursorBinary, r.cursorArguments(args...)...)
	command.Env = accountEnvironment("cursor", home)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		return "", fmt.Errorf("Cursor CLI exited: %s: %w", tail(output.String(), 4000), err)
	}
	return output.String(), nil
}

func (r *Runtimes) cursorArguments(args ...string) []string {
	if r.cursorScript == "" {
		return args
	}
	return append([]string{"--use-system-ca", r.cursorScript}, args...)
}

func (r *Runtimes) removeLogin(state string) {
	r.mu.Lock()
	delete(r.logins, state)
	r.mu.Unlock()
}

func (r *Runtimes) expireLogin(state string, flow *loginFlow) {
	timer := time.NewTimer(15 * time.Minute)
	defer timer.Stop()
	<-timer.C

	r.mu.Lock()
	if r.logins[state] != flow {
		r.mu.Unlock()
		return
	}
	delete(r.logins, state)
	r.mu.Unlock()
	flow.stop()
}

func accountEnvironment(provider, home string) []string {
	environment := append([]string{}, os.Environ()...)
	environment = setEnvironment(environment, "HOME", home)
	environment = setEnvironment(environment, "NO_OPEN_BROWSER", "1")
	environment = setEnvironment(environment, "BROWSER", "echo")
	environment = setEnvironment(environment, "GH_BROWSER", "echo")
	if provider == "copilot" {
		environment = setEnvironment(environment, "COPILOT_HOME", filepath.Join(home, ".copilot"))
	} else {
		environment = setEnvironment(environment, "CURSOR_INVOKED_AS", "cursor-agent")
		environment = setEnvironment(environment, "NODE_COMPILE_CACHE", filepath.Join(home, ".cache", "cursor-compile-cache"))
	}
	return environment
}

func setEnvironment(environment []string, name, value string) []string {
	prefix := name + "="
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

func tail(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[len(value)-limit:]
}
