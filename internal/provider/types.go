package provider

import (
	"encoding/json"
	"net/http"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

type Kind string

const (
	KindCopilot Kind = "copilot"
	KindCursor  Kind = "cursor"
)

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type registration struct {
	SchemaVersion uint32                 `json:"schema_version"`
	Metadata      pluginapi.Metadata     `json:"metadata"`
	Capabilities  registrationCapability `json:"capabilities"`
}

type registrationCapability struct {
	ModelProvider         bool                         `json:"model_provider"`
	AuthProvider          bool                         `json:"auth_provider"`
	Executor              bool                         `json:"executor"`
	ExecutorModelScope    pluginapi.ExecutorModelScope `json:"executor_model_scope"`
	ExecutorInputFormats  []string                     `json:"executor_input_formats"`
	ExecutorOutputFormats []string                     `json:"executor_output_formats"`
	ManagementAPI         bool                         `json:"management_api"`
}

type identifierResponse struct {
	Identifier string `json:"identifier"`
}

type streamResponse struct {
	Headers http.Header                     `json:"headers,omitempty"`
	Chunks  []pluginapi.ExecutorStreamChunk `json:"chunks,omitempty"`
}

type storedAuth struct {
	Type      string `json:"type"`
	Upstream  Kind   `json:"upstream"`
	Handle    string `json:"handle"`
	Label     string `json:"label,omitempty"`
	Login     string `json:"login,omitempty"`
	CreatedAt string `json:"created_at,omitempty"`
}

type bridgeLoginStart struct {
	Provider string `json:"provider"`
}

type bridgeLoginStartResponse struct {
	URL       string         `json:"url"`
	State     string         `json:"state"`
	ExpiresAt string         `json:"expires_at"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type bridgeLoginPoll struct {
	Provider string `json:"provider"`
	State    string `json:"state"`
}

type bridgeLoginPollResponse struct {
	Status  string     `json:"status"`
	Message string     `json:"message,omitempty"`
	Auth    storedAuth `json:"auth,omitempty"`
}

type bridgeModelsRequest struct {
	Provider string `json:"provider"`
	Handle   string `json:"handle"`
}

type bridgeModelsResponse struct {
	Models []bridgeModel `json:"models"`
}

type bridgeModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
	Description string `json:"description,omitempty"`
	Context     int64  `json:"context_length,omitempty"`
	Output      int64  `json:"max_completion_tokens,omitempty"`
}

type bridgeExecuteRequest struct {
	Provider string          `json:"provider"`
	Handle   string          `json:"handle"`
	Model    string          `json:"model"`
	Stream   bool            `json:"stream"`
	Payload  json.RawMessage `json:"payload"`
}

type bridgeExecuteResponse struct {
	Payload json.RawMessage `json:"payload"`
	Chunks  []string        `json:"chunks,omitempty"`
}

type managementRegistration struct {
	Resources []managementResource `json:"resources,omitempty"`
}

type managementResource struct {
	Path        string `json:"Path"`
	Menu        string `json:"Menu"`
	Description string `json:"Description"`
}

type hostAuthListResponse struct {
	Files []pluginapi.HostAuthFileEntry `json:"files"`
}

type quotaAccount struct {
	Entry pluginapi.HostAuthFileEntry
	Auth  storedAuth
	Quota json.RawMessage
	Error string
}
