package bridge

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

type Options struct {
	DataDir       string
	CopilotBinary string
	CursorBinary  string
	Secret        string
}

type Server struct {
	secret   string
	runtimes *Runtimes
}

func NewServer(options Options) (*Server, error) {
	store, err := NewCredentialStore(options.DataDir)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(options.CopilotBinary); err != nil {
		return nil, fmt.Errorf("Copilot CLI is unavailable: %w", err)
	}
	if _, err := os.Stat(options.CursorBinary); err != nil {
		return nil, fmt.Errorf("Cursor Agent is unavailable: %w", err)
	}
	return &Server{
		secret: strings.TrimSpace(options.Secret),
		runtimes: NewRuntimes(
			store,
			options.CopilotBinary,
			options.CursorBinary,
		),
	}, nil
}

func (s *Server) ListenAndServe(address string) error {
	server := &http.Server{
		Addr:              address,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      180 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	return server.ListenAndServe()
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writeJSON(writer, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /v1/oauth/start", s.protected(s.oauthStart))
	mux.HandleFunc("POST /v1/oauth/poll", s.protected(s.oauthPoll))
	mux.HandleFunc("POST /v1/models", s.protected(s.models))
	mux.HandleFunc("POST /v1/execute", s.protected(s.execute))
	mux.HandleFunc("POST /v1/quota", s.protected(s.quota))
	return mux
}

func (s *Server) oauthStart(writer http.ResponseWriter, request *http.Request) error {
	var payload struct {
		Provider string `json:"provider"`
	}
	if err := decodeJSON(writer, request, &payload); err != nil {
		return err
	}
	provider, err := parseProvider(payload.Provider)
	if err != nil {
		return badRequest(err)
	}
	url, state, metadata, err := s.runtimes.StartLogin(request.Context(), provider)
	if err != nil {
		return err
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"url": url, "state": state,
		"expires_at": time.Now().UTC().Add(15 * time.Minute).Format(time.RFC3339),
		"metadata":   metadata,
	})
	return nil
}

func (s *Server) oauthPoll(writer http.ResponseWriter, request *http.Request) error {
	var payload struct {
		Provider string `json:"provider"`
		State    string `json:"state"`
	}
	if err := decodeJSON(writer, request, &payload); err != nil {
		return err
	}
	provider, err := parseProvider(payload.Provider)
	if err != nil {
		return badRequest(err)
	}
	status, message, auth := s.runtimes.PollLogin(provider, payload.State)
	response := map[string]any{"status": status, "message": message}
	if auth != nil {
		response["auth"] = auth
	}
	writeJSON(writer, http.StatusOK, response)
	return nil
}

func (s *Server) models(writer http.ResponseWriter, request *http.Request) error {
	var payload struct {
		Provider string `json:"provider"`
		Handle   string `json:"handle"`
	}
	if err := decodeJSON(writer, request, &payload); err != nil {
		return err
	}
	provider, err := parseProvider(payload.Provider)
	if err != nil {
		return badRequest(err)
	}
	if err := validateProviderHandle(provider, payload.Handle); err != nil {
		return badRequest(err)
	}
	models, err := s.runtimes.ListModels(request.Context(), provider, payload.Handle)
	if err != nil {
		return err
	}
	writeJSON(writer, http.StatusOK, map[string]any{"models": models})
	return nil
}

func (s *Server) execute(writer http.ResponseWriter, request *http.Request) error {
	var payload struct {
		Provider string         `json:"provider"`
		Handle   string         `json:"handle"`
		Model    string         `json:"model"`
		Stream   bool           `json:"stream"`
		Payload  map[string]any `json:"payload"`
	}
	if err := decodeJSON(writer, request, &payload); err != nil {
		return err
	}
	provider, err := parseProvider(payload.Provider)
	if err != nil {
		return badRequest(err)
	}
	if payload.Payload == nil {
		return badRequest(errors.New("payload must be a chat-completions JSON object"))
	}
	if err := validateProviderHandle(provider, payload.Handle); err != nil {
		return badRequest(err)
	}
	response, err := s.runtimes.Execute(
		request.Context(), provider, payload.Handle, payload.Model, payload.Payload, payload.Stream,
	)
	if err != nil {
		return err
	}
	writeJSON(writer, http.StatusOK, response)
	return nil
}

func (s *Server) quota(writer http.ResponseWriter, request *http.Request) error {
	var payload struct {
		Provider string `json:"provider"`
		Handle   string `json:"handle"`
	}
	if err := decodeJSON(writer, request, &payload); err != nil {
		return err
	}
	provider, err := parseProvider(payload.Provider)
	if err != nil {
		return badRequest(err)
	}
	if err := validateProviderHandle(provider, payload.Handle); err != nil {
		return badRequest(err)
	}
	quota, err := s.runtimes.Quota(request.Context(), provider, payload.Handle)
	if err != nil {
		return err
	}
	writeJSON(writer, http.StatusOK, quota)
	return nil
}

type handler func(http.ResponseWriter, *http.Request) error

func (s *Server) protected(next handler) http.HandlerFunc {
	return func(writer http.ResponseWriter, request *http.Request) {
		if s.secret != "" && request.Header.Get("Authorization") != "Bearer "+s.secret {
			writeJSON(writer, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if err := next(writer, request); err != nil {
			status := http.StatusBadGateway
			var requestError *httpError
			if errors.As(err, &requestError) {
				status = requestError.status
			} else if errors.Is(err, os.ErrNotExist) {
				status = http.StatusNotFound
			}
			writeJSON(writer, status, map[string]string{"error": err.Error()})
		}
	}
}

type httpError struct {
	status int
	err    error
}

func (e *httpError) Error() string { return e.err.Error() }
func (e *httpError) Unwrap() error { return e.err }

func badRequest(err error) error {
	return &httpError{status: http.StatusBadRequest, err: err}
}

func decodeJSON(writer http.ResponseWriter, request *http.Request, target any) error {
	request.Body = http.MaxBytesReader(writer, request.Body, 16<<20)
	decoder := json.NewDecoder(request.Body)
	if err := decoder.Decode(target); err != nil {
		return badRequest(fmt.Errorf("invalid JSON: %w", err))
	}
	return nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func parseProvider(value string) (string, error) {
	provider := strings.ToLower(strings.TrimSpace(value))
	if provider != "copilot" && provider != "cursor" {
		return "", errors.New("provider must be copilot or cursor")
	}
	return provider, nil
}
