package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func Handle(kind Kind, method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		return okEnvelope(registrationFor(kind))
	case pluginabi.MethodModelStatic:
		return okEnvelope(pluginapi.ModelResponse{Provider: providerID(kind), Models: staticModels(kind)})
	case pluginabi.MethodModelForAuth:
		return modelsForAuth(kind, request)
	case pluginabi.MethodAuthIdentifier, pluginabi.MethodExecutorIdentifier:
		return okEnvelope(identifierResponse{Identifier: providerID(kind)})
	case pluginabi.MethodAuthParse:
		return parseAuth(kind, request)
	case pluginabi.MethodAuthLoginStart:
		return startLogin(kind)
	case pluginabi.MethodAuthLoginPoll:
		return pollLogin(kind, request)
	case pluginabi.MethodAuthRefresh:
		return refreshAuth(kind, request)
	case pluginabi.MethodExecutorExecute:
		return execute(kind, request, false)
	case pluginabi.MethodExecutorExecuteStream:
		return execute(kind, request, true)
	case pluginabi.MethodExecutorCountTokens:
		return okEnvelope(pluginapi.ExecutorResponse{Payload: []byte(`{"total_tokens":0}`)})
	case pluginabi.MethodManagementRegister:
		return okEnvelope(managementRegistration{Resources: managementResources(kind)})
	case pluginabi.MethodManagementHandle:
		return handleManagement(kind, request)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func HandleCatalog(method string) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		return okEnvelope(registration{
			SchemaVersion: pluginabi.SchemaVersion,
			Metadata: pluginapi.Metadata{
				Name:             "GitHub Copilot Model Catalog",
				Version:          "0.2.0-rc.13",
				Author:           "linonetwo",
				GitHubRepository: "https://github.com/linonetwo/cpa-copilot-cursor",
				Logo:             "https://raw.githubusercontent.com/linonetwo/cpa-copilot-cursor/main/assets/logo.svg",
			},
			Capabilities: registrationCapability{ModelProvider: true},
		})
	case pluginabi.MethodModelStatic:
		return okEnvelope(pluginapi.ModelResponse{Provider: providerID(KindCopilot), Models: staticModels(KindCopilot)})
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func registrationFor(kind Kind) registration {
	modelScope := pluginapi.ExecutorModelScopeOAuth
	if kind == KindCopilot {
		modelScope = pluginapi.ExecutorModelScopeBoth
	}
	return registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             providerName(kind),
			Version:          "0.2.0-rc.13",
			Author:           "linonetwo",
			GitHubRepository: "https://github.com/linonetwo/cpa-copilot-cursor",
			Logo:             "https://raw.githubusercontent.com/linonetwo/cpa-copilot-cursor/main/assets/logo.svg",
		},
		Capabilities: registrationCapability{
			ModelProvider:         true,
			AuthProvider:          true,
			Executor:              true,
			ExecutorModelScope:    modelScope,
			ExecutorInputFormats:  []string{"chat-completions"},
			ExecutorOutputFormats: []string{"chat-completions"},
			ManagementAPI:         true,
		},
	}
}

func staticModels(kind Kind) []pluginapi.ModelInfo {
	if kind != KindCopilot {
		return nil
	}
	catalog := []struct {
		id      string
		display string
		context int64
	}{
		{"auto", "Auto", 0},
		{"gpt-5.5", "GPT-5.5", 1_050_000},
		{"gpt-5.4", "GPT-5.4", 1_050_000},
		{"gpt-5.3-codex", "GPT-5.3-Codex", 400_000},
		{"gpt-5.4-mini", "GPT-5.4 mini", 400_000},
		{"gpt-5-mini", "GPT-5 mini", 264_000},
		{"gemini-3.1-pro-preview", "Gemini 3.1 Pro", 1_000_000},
		{"gemini-3.5-flash", "Gemini 3.5 Flash", 1_000_000},
		{"mai-code-1-flash-picker", "MAI-Code-1-Flash", 256_000},
	}
	models := make([]pluginapi.ModelInfo, 0, len(catalog))
	for _, model := range catalog {
		models = append(models, pluginapi.ModelInfo{
			ID:                         model.id,
			Name:                       model.id,
			Object:                     "model",
			OwnedBy:                    providerID(kind),
			DisplayName:                model.display,
			ContextLength:              model.context,
			SupportedGenerationMethods: []string{"chat"},
			SupportedInputModalities:   []string{"text"},
			SupportedOutputModalities:  []string{"text"},
		})
	}
	return models
}

func parseAuth(kind Kind, request []byte) ([]byte, error) {
	var parseRequest pluginapi.AuthParseRequest
	if err := json.Unmarshal(request, &parseRequest); err != nil {
		return nil, err
	}
	var auth storedAuth
	if err := json.Unmarshal(parseRequest.RawJSON, &auth); err != nil || !validStoredAuth(kind, auth) {
		return okEnvelope(pluginapi.AuthParseResponse{Handled: false})
	}
	return okEnvelope(pluginapi.AuthParseResponse{Handled: true, Auth: authData(kind, auth)})
}

func startLogin(kind Kind) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), bridgeTimeout())
	defer cancel()
	var response bridgeLoginStartResponse
	if err := callBridge(ctx, "/v1/oauth/start", bridgeLoginStart{Provider: string(kind)}, &response); err != nil {
		return nil, err
	}
	expiresAt, err := time.Parse(time.RFC3339, response.ExpiresAt)
	if err != nil {
		expiresAt = time.Now().Add(10 * time.Minute).UTC()
	}
	authorizationURL := response.URL
	if rememberDeviceFlow(kind, response.State, response.Metadata, expiresAt) {
		authorizationURL = deviceFlowURL(kind, response.State)
	}
	return okEnvelope(pluginapi.AuthLoginStartResponse{
		Provider:  providerID(kind),
		URL:       authorizationURL,
		State:     response.State,
		ExpiresAt: expiresAt,
		Metadata:  response.Metadata,
	})
}

func pollLogin(kind Kind, request []byte) ([]byte, error) {
	var pollRequest pluginapi.AuthLoginPollRequest
	if err := json.Unmarshal(request, &pollRequest); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), bridgeTimeout())
	defer cancel()
	var response bridgeLoginPollResponse
	if err := callBridge(ctx, "/v1/oauth/poll", bridgeLoginPoll{Provider: string(kind), State: pollRequest.State}, &response); err != nil {
		return nil, err
	}
	switch response.Status {
	case "success":
		forgetDeviceFlow(kind, pollRequest.State)
		return okEnvelope(pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusSuccess, Message: response.Message, Auth: authData(kind, response.Auth)})
	case "error":
		forgetDeviceFlow(kind, pollRequest.State)
		return okEnvelope(pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusError, Message: response.Message})
	default:
		return okEnvelope(pluginapi.AuthLoginPollResponse{Status: pluginapi.AuthLoginStatusPending, Message: response.Message})
	}
}

func refreshAuth(kind Kind, request []byte) ([]byte, error) {
	var refreshRequest pluginapi.AuthRefreshRequest
	if err := json.Unmarshal(request, &refreshRequest); err != nil {
		return nil, err
	}
	var auth storedAuth
	if err := json.Unmarshal(refreshRequest.StorageJSON, &auth); err != nil {
		return nil, err
	}
	return okEnvelope(pluginapi.AuthRefreshResponse{Auth: authData(kind, auth), NextRefreshAfter: time.Now().Add(30 * time.Minute).UTC()})
}

func modelsForAuth(kind Kind, request []byte) ([]byte, error) {
	var modelRequest pluginapi.AuthModelRequest
	if err := json.Unmarshal(request, &modelRequest); err != nil {
		return nil, err
	}
	auth, err := decodeStoredAuth(kind, modelRequest.StorageJSON)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), bridgeTimeout())
	defer cancel()
	var response bridgeModelsResponse
	if err := callBridge(ctx, "/v1/models", bridgeModelsRequest{Provider: string(kind), Handle: auth.Handle}, &response); err != nil {
		return nil, err
	}
	models := make([]pluginapi.ModelInfo, 0, len(response.Models))
	for _, model := range response.Models {
		if strings.TrimSpace(model.ID) == "" {
			continue
		}
		models = append(models, pluginapi.ModelInfo{
			ID:                         model.ID,
			Name:                       model.ID,
			Object:                     "model",
			OwnedBy:                    providerID(kind),
			DisplayName:                firstNonEmpty(model.DisplayName, model.ID),
			Description:                model.Description,
			ContextLength:              model.Context,
			MaxCompletionTokens:        model.Output,
			SupportedGenerationMethods: []string{"chat"},
			SupportedInputModalities:   []string{"text"},
			SupportedOutputModalities:  []string{"text"},
		})
	}
	return okEnvelope(pluginapi.ModelResponse{Provider: providerID(kind), Models: models})
}

func execute(kind Kind, request []byte, stream bool) ([]byte, error) {
	var executorRequest pluginapi.ExecutorRequest
	if err := json.Unmarshal(request, &executorRequest); err != nil {
		return nil, err
	}
	auth, err := decodeStoredAuth(kind, executorRequest.StorageJSON)
	if err != nil {
		return nil, err
	}
	payload := executorRequest.Payload
	if len(payload) == 0 {
		payload = executorRequest.OriginalRequest
	}
	if len(payload) == 0 || !json.Valid(payload) {
		return nil, fmt.Errorf("executor request contains no valid JSON payload")
	}
	ctx, cancel := context.WithTimeout(context.Background(), bridgeTimeout())
	defer cancel()
	var response bridgeExecuteResponse
	if err := callBridge(ctx, "/v1/execute", bridgeExecuteRequest{
		Provider: string(kind),
		Handle:   auth.Handle,
		Model:    executorRequest.Model,
		Stream:   stream,
		Payload:  payload,
	}, &response); err != nil {
		return nil, err
	}
	if !stream {
		return okEnvelope(pluginapi.ExecutorResponse{Payload: response.Payload, Headers: http.Header{"Content-Type": []string{"application/json"}}})
	}
	chunks := make([]pluginapi.ExecutorStreamChunk, 0, len(response.Chunks))
	for _, chunk := range response.Chunks {
		chunks = append(chunks, pluginapi.ExecutorStreamChunk{Payload: []byte(chunk)})
	}
	return okEnvelope(streamResponse{Headers: http.Header{"Content-Type": []string{"text/event-stream"}}, Chunks: chunks})
}

func authData(kind Kind, auth storedAuth) pluginapi.AuthData {
	storage, _ := json.Marshal(auth)
	label := firstNonEmpty(auth.Label, auth.Login, providerName(kind))
	return pluginapi.AuthData{
		Provider:    providerID(kind),
		ID:          providerID(kind) + "-" + auth.Handle,
		FileName:    providerID(kind) + "-" + auth.Handle + ".json",
		Label:       label,
		StorageJSON: storage,
		Metadata: map[string]any{
			"type":       "copilot-cursor",
			"upstream":   string(kind),
			"login":      auth.Login,
			"created_at": auth.CreatedAt,
		},
		Attributes: map[string]string{"upstream": string(kind), "handle": auth.Handle},
	}
}

func decodeStoredAuth(kind Kind, storage []byte) (storedAuth, error) {
	var auth storedAuth
	if err := json.Unmarshal(storage, &auth); err != nil {
		return auth, err
	}
	if !validStoredAuth(kind, auth) {
		return auth, fmt.Errorf("invalid %s subscription auth", providerID(kind))
	}
	return auth, nil
}

func validStoredAuth(kind Kind, auth storedAuth) bool {
	validType := auth.Type == "copilot-cursor" || auth.Type == providerID(kind)
	if kind == KindCopilot && auth.Type == "github-copilot" {
		validType = true
	}
	return auth.Upstream == kind &&
		auth.Handle != "" &&
		validType
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func okEnvelope(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope{OK: true, Result: raw})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}
