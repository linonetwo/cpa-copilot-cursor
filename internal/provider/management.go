package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

type HostCaller func(method string, payload any) (json.RawMessage, error)

var hostCaller HostCaller

func SetHostCaller(caller HostCaller) {
	hostCaller = caller
}

func managementResources(kind Kind) []managementResource {
	resources := []managementResource{{
		Path:        "/quota",
		Menu:        providerName(kind) + " Quota",
		Description: "Shows subscription quota and account health without exposing OAuth credentials.",
	}}
	if kind == KindCopilot {
		resources = append(resources, managementResource{
			Path:        "/device",
			Description: "Displays the GitHub device code for a pending Copilot login.",
		})
	}
	return resources
}

func handleManagement(kind Kind, request []byte) ([]byte, error) {
	var managementRequest pluginapi.ManagementRequest
	if len(request) > 0 {
		if err := json.Unmarshal(request, &managementRequest); err != nil {
			return nil, err
		}
	}
	if strings.HasSuffix(strings.TrimRight(managementRequest.Path, "/"), "/device") {
		return handleDeviceFlow(kind, managementRequest)
	}
	accounts, err := loadQuotaAccounts(kind)
	if err != nil {
		return okEnvelope(htmlResponse(http.StatusBadGateway, renderQuotaPage(kind, nil, err.Error())))
	}
	return okEnvelope(htmlResponse(http.StatusOK, renderQuotaPage(kind, accounts, "")))
}

func loadQuotaAccounts(kind Kind) ([]quotaAccount, error) {
	if hostCaller == nil {
		return nil, fmt.Errorf("CPA host callback is unavailable")
	}
	result, err := hostCaller(pluginabi.MethodHostAuthList, map[string]any{})
	if err != nil {
		return nil, err
	}
	var list hostAuthListResponse
	if err := json.Unmarshal(result, &list); err != nil {
		return nil, fmt.Errorf("decode auth list: %w", err)
	}
	accounts := make([]quotaAccount, 0)
	for _, entry := range list.Files {
		if entry.Provider != providerID(kind) || strings.TrimSpace(entry.AuthIndex) == "" {
			continue
		}
		account := quotaAccount{Entry: entry}
		getResult, err := hostCaller(pluginabi.MethodHostAuthGet, pluginapi.HostAuthGetRequest{AuthIndex: entry.AuthIndex})
		if err != nil {
			account.Error = err.Error()
			accounts = append(accounts, account)
			continue
		}
		var getResponse pluginapi.HostAuthGetResponse
		if err := json.Unmarshal(getResult, &getResponse); err != nil {
			account.Error = err.Error()
			accounts = append(accounts, account)
			continue
		}
		if err := json.Unmarshal(getResponse.JSON, &account.Auth); err != nil {
			account.Error = err.Error()
			accounts = append(accounts, account)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), bridgeTimeout())
		var quota json.RawMessage
		err = callBridge(ctx, "/v1/quota", bridgeModelsRequest{Provider: string(kind), Handle: account.Auth.Handle}, &quota)
		cancel()
		if err != nil {
			account.Error = err.Error()
		} else {
			account.Quota = quota
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func htmlResponse(status int, body []byte) pluginapi.ManagementResponse {
	return pluginapi.ManagementResponse{
		StatusCode: status,
		Headers: http.Header{
			"Content-Type":            []string{"text/html; charset=utf-8"},
			"Cache-Control":           []string{"no-store"},
			"Content-Security-Policy": []string{"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; frame-ancestors 'self'"},
			"Referrer-Policy":         []string{"no-referrer"},
			"X-Content-Type-Options":  []string{"nosniff"},
		},
		Body: body,
	}
}

func renderQuotaPage(kind Kind, accounts []quotaAccount, errorText string) []byte {
	var output bytes.Buffer
	output.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">")
	output.WriteString("<title>Subscription Quota</title><style>body{font-family:ui-sans-serif,system-ui,-apple-system,sans-serif;margin:0;background:#0b1020;color:#e5e7eb}main{max-width:1100px;margin:auto;padding:32px}h1{margin:0 0 8px}.muted{color:#9ca3af}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(320px,1fr));gap:16px;margin-top:24px}.card{background:#151b2e;border:1px solid #2b3553;border-radius:14px;padding:18px;box-shadow:0 10px 30px #0003}.row{display:flex;justify-content:space-between;gap:16px;margin:8px 0}.status{padding:3px 9px;border-radius:999px;background:#233153}.error{color:#fca5a5;white-space:pre-wrap}pre{white-space:pre-wrap;word-break:break-word;background:#0b1020;border-radius:10px;padding:12px;overflow:auto}a{color:#93c5fd}</style></head><body><main>")
	output.WriteString("<h1>" + html.EscapeString(providerName(kind)) + " Quota</h1>")
	output.WriteString("<p class=\"muted\">OAuth 登录入口位于 CPA 的“OAuth 登录”页面。此页仅展示额度与状态，不展示令牌。</p>")
	if errorText != "" {
		output.WriteString("<p class=\"error\">" + html.EscapeString(errorText) + "</p>")
	}
	output.WriteString("<div class=\"grid\">")
	if len(accounts) == 0 && errorText == "" {
		output.WriteString("<section class=\"card\"><p>尚未找到已登录账号。</p></section>")
	}
	for _, account := range accounts {
		output.WriteString("<section class=\"card\">")
		label := firstNonEmpty(account.Entry.Label, account.Auth.Label, account.Entry.Name)
		output.WriteString("<h2>" + html.EscapeString(label) + "</h2>")
		output.WriteString("<div class=\"row\"><span>状态</span><span class=\"status\">" + html.EscapeString(firstNonEmpty(account.Entry.Status, "unknown")) + "</span></div>")
		if account.Auth.Login != "" {
			output.WriteString("<div class=\"row\"><span>账号</span><span>" + html.EscapeString(account.Auth.Login) + "</span></div>")
		}
		if account.Error != "" {
			output.WriteString("<p class=\"error\">" + html.EscapeString(account.Error) + "</p>")
		} else {
			output.WriteString("<pre>" + html.EscapeString(prettyJSON(account.Quota)) + "</pre>")
		}
		output.WriteString("</section>")
	}
	output.WriteString("</div></main></body></html>")
	return output.Bytes()
}

func prettyJSON(raw json.RawMessage) string {
	var output bytes.Buffer
	if json.Indent(&output, raw, "", "  ") == nil {
		return output.String()
	}
	return string(raw)
}
