package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

func callBridge(ctx context.Context, path string, requestBody any, responseBody any) error {
	body, err := json.Marshal(requestBody)
	if err != nil {
		return fmt.Errorf("encode bridge request: %w", err)
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, bridgeEndpoint()+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create bridge request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if secret := bridgeSecret(); secret != "" {
		request.Header.Set("Authorization", "Bearer "+secret)
	}

	client := &http.Client{Timeout: bridgeTimeout()}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("call Copilot/Cursor bridge: %w", err)
	}
	defer response.Body.Close()

	responseBytes, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("read Copilot/Cursor bridge response: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("Copilot/Cursor bridge returned %d: %s", response.StatusCode, string(responseBytes))
	}
	if responseBody == nil {
		return nil
	}
	if err := json.Unmarshal(responseBytes, responseBody); err != nil {
		return fmt.Errorf("decode Copilot/Cursor bridge response: %w", err)
	}
	return nil
}
