package bridge

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var handlePattern = regexp.MustCompile(`^[A-Za-z0-9]{1,80}$`)

type AuthRecord struct {
	Type      string `json:"type"`
	Upstream  string `json:"upstream"`
	Handle    string `json:"handle"`
	Label     string `json:"label"`
	Login     string `json:"login"`
	CreatedAt string `json:"created_at"`
}

type CredentialStore struct {
	root string
}

func NewCredentialStore(root string) (*CredentialStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("credential store root is required")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("create credential store: %w", err)
	}
	return &CredentialStore{root: root}, nil
}

func (s *CredentialStore) NewHandle() (string, error) {
	value := make([]byte, 18)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate handle: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func (s *CredentialStore) AccountDir(provider, handle string) (string, error) {
	if err := validateProviderHandle(provider, handle); err != nil {
		return "", err
	}
	path := filepath.Join(s.root, provider, handle)
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("create account directory: %w", err)
	}
	return path, nil
}

func (s *CredentialStore) HomeDir(provider, handle string) (string, error) {
	accountDir, err := s.AccountDir(provider, handle)
	if err != nil {
		return "", err
	}
	path := filepath.Join(accountDir, "home")
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("create home directory: %w", err)
	}
	return path, nil
}

func (s *CredentialStore) SaveRecord(provider, handle, label, login string) (AuthRecord, error) {
	accountDir, err := s.AccountDir(provider, handle)
	if err != nil {
		return AuthRecord{}, err
	}
	if label == "" {
		label = strings.ToUpper(provider[:1]) + provider[1:] + " subscription"
	}
	record := AuthRecord{
		Type:      "copilot-cursor",
		Upstream:  provider,
		Handle:    handle,
		Label:     label,
		Login:     login,
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	content, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return AuthRecord{}, fmt.Errorf("encode auth record: %w", err)
	}
	content = append(content, '\n')
	path := filepath.Join(accountDir, "auth.json")
	tempPath := path + ".tmp"
	if err := os.WriteFile(tempPath, content, 0o600); err != nil {
		return AuthRecord{}, fmt.Errorf("write auth record: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return AuthRecord{}, fmt.Errorf("replace auth record: %w", err)
	}
	return record, nil
}

func (s *CredentialStore) LoadRecord(provider, handle string) (AuthRecord, error) {
	accountDir, err := s.AccountDir(provider, handle)
	if err != nil {
		return AuthRecord{}, err
	}
	content, err := os.ReadFile(filepath.Join(accountDir, "auth.json"))
	if err != nil {
		return AuthRecord{}, err
	}
	var record AuthRecord
	if err := json.Unmarshal(content, &record); err != nil {
		return AuthRecord{}, fmt.Errorf("decode auth record: %w", err)
	}
	return record, nil
}

func (s *CredentialStore) AssertAccount(provider, handle string) error {
	_, err := s.LoadRecord(provider, handle)
	return err
}

func validateProviderHandle(provider, handle string) error {
	if provider != "copilot" && provider != "cursor" {
		return errors.New("unsupported provider")
	}
	if !handlePattern.MatchString(handle) {
		return errors.New("invalid account handle")
	}
	return nil
}
