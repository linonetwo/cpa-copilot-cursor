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
	menu := "Copilot 配额"
	description := "展示订阅额度与账号状态，不暴露 OAuth 凭证。"
	if kind == KindCursor {
		menu = "Cursor 账号状态"
		description = "展示 Cursor 登录状态，并提供官方用量页面入口。"
	}
	resources := []managementResource{{
		Path:        "/quota",
		Menu:        menu,
		Description: description,
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
	chinese := isChinese(managementRequest.Headers)
	accounts, err := loadQuotaAccounts(kind)
	if err != nil {
		return okEnvelope(htmlResponse(http.StatusBadGateway, renderQuotaPage(kind, nil, err.Error(), chinese)))
	}
	return okEnvelope(htmlResponse(http.StatusOK, renderQuotaPage(kind, accounts, "", chinese)))
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

func renderQuotaPage(kind Kind, accounts []quotaAccount, errorText string, chinese bool) []byte {
	var output bytes.Buffer
	output.WriteString("<!doctype html><html><head><meta charset=\"utf-8\"><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\">")
	output.WriteString("<title>Subscription Status</title><style>body{font-family:ui-sans-serif,system-ui,-apple-system,sans-serif;margin:0;background:#0b1020;color:#e5e7eb}main{max-width:1100px;margin:auto;padding:32px}h1{margin:0 0 8px}.muted{color:#9ca3af}.grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(320px,1fr));gap:16px;margin-top:24px}.card{background:#151b2e;border:1px solid #2b3553;border-radius:14px;padding:18px;box-shadow:0 10px 30px #0003}.row{display:flex;justify-content:space-between;gap:16px;margin:8px 0}.status{padding:3px 9px;border-radius:999px;background:#233153}.error{color:#fca5a5;white-space:pre-wrap}.quota{margin-top:18px}.quota-head{display:flex;justify-content:space-between;gap:12px}.track{height:10px;background:#27324d;border-radius:999px;overflow:hidden;margin:8px 0}.fill{height:100%;background:#60a5fa}.notice{background:#0b1020;border-radius:10px;padding:14px;margin-top:16px}a{color:#93c5fd}</style></head><body><main>")
	title := "GitHub Copilot Subscription Quota"
	intro := "Sign in from CPA's OAuth page. This page shows quota and account status without exposing credentials."
	statusLabel := "Status"
	accountLabel := "Account"
	noAccounts := "No signed-in accounts found."
	if kind == KindCursor {
		title = "Cursor Subscription Status"
		intro = "Cursor's official CLI exposes login status but not subscription quota. Open the official Usage page for detailed usage."
	}
	if chinese {
		title = "GitHub Copilot 订阅配额"
		intro = "OAuth 登录入口位于 CPA 的“OAuth 登录”页面。此页仅展示额度与状态，不展示凭证。"
		statusLabel = "状态"
		accountLabel = "账号"
		noAccounts = "尚未找到已登录账号。"
		if kind == KindCursor {
			title = "Cursor 订阅状态"
			intro = "Cursor 官方 CLI 目前只提供登录状态，不提供订阅额度。详细用量请前往 Cursor 官方 Usage 页面查看。"
		}
	}
	output.WriteString("<h1>" + html.EscapeString(title) + "</h1>")
	output.WriteString("<p class=\"muted\">" + html.EscapeString(intro) + "</p>")
	if errorText != "" {
		output.WriteString("<p class=\"error\">" + html.EscapeString(errorText) + "</p>")
	}
	output.WriteString("<div class=\"grid\">")
	if len(accounts) == 0 && errorText == "" {
		output.WriteString("<section class=\"card\"><p>" + html.EscapeString(noAccounts) + "</p></section>")
	}
	for _, account := range accounts {
		output.WriteString("<section class=\"card\">")
		label := firstNonEmpty(account.Entry.Label, account.Auth.Label, account.Entry.Name)
		output.WriteString("<h2>" + html.EscapeString(label) + "</h2>")
		output.WriteString("<div class=\"row\"><span>" + html.EscapeString(statusLabel) + "</span><span class=\"status\">" + html.EscapeString(firstNonEmpty(account.Entry.Status, "unknown")) + "</span></div>")
		if account.Auth.Login != "" {
			output.WriteString("<div class=\"row\"><span>" + html.EscapeString(accountLabel) + "</span><span>" + html.EscapeString(account.Auth.Login) + "</span></div>")
		}
		if account.Error != "" {
			output.WriteString("<p class=\"error\">" + html.EscapeString(account.Error) + "</p>")
		} else if kind == KindCopilot {
			renderCopilotQuota(&output, account.Quota, chinese)
		} else {
			renderCursorStatus(&output, account.Quota, chinese)
		}
		output.WriteString("</section>")
	}
	output.WriteString("</div></main></body></html>")
	return output.Bytes()
}

func renderCopilotQuota(output *bytes.Buffer, raw json.RawMessage, chinese bool) {
	var payload struct {
		QuotaSnapshots map[string]struct {
			EntitlementRequests int     `json:"entitlementRequests"`
			IsUnlimited         bool    `json:"isUnlimitedEntitlement"`
			RemainingPercentage float64 `json:"remainingPercentage"`
			ResetDate           string  `json:"resetDate"`
			UsedRequests        int     `json:"usedRequests"`
		} `json:"quotaSnapshots"`
	}
	if json.Unmarshal(raw, &payload) != nil || len(payload.QuotaSnapshots) == 0 {
		message := "Unable to parse Copilot quota data."
		if chinese {
			message = "暂时无法解析 Copilot 额度数据。"
		}
		output.WriteString("<p class=\"error\">" + message + "</p>")
		return
	}
	labels := map[string]string{"chat": "Chat", "completions": "Completions", "premium_interactions": "Premium requests"}
	for _, key := range []string{"premium_interactions", "chat", "completions"} {
		quota, ok := payload.QuotaSnapshots[key]
		if !ok {
			continue
		}
		summary := fmt.Sprintf("%d / %d", quota.UsedRequests, quota.EntitlementRequests)
		if quota.IsUnlimited {
			summary = "Unlimited"
			if chinese {
				summary = "无限"
			}
		}
		remaining := fmt.Sprintf("%.0f%% remaining", quota.RemainingPercentage)
		reset := "Resets: "
		if chinese {
			remaining = "剩余 " + fmt.Sprintf("%.0f%%", quota.RemainingPercentage)
			reset = "重置时间："
		}
		output.WriteString("<div class=\"quota\"><div class=\"quota-head\"><strong>" + labels[key] + "</strong><span>" + html.EscapeString(summary) + " · " + remaining + "</span></div>")
		output.WriteString("<div class=\"track\"><div class=\"fill\" style=\"width:" + fmt.Sprintf("%.2f%%", quota.RemainingPercentage) + "\"></div></div>")
		if quota.ResetDate != "" {
			output.WriteString("<div class=\"muted\">" + reset + html.EscapeString(quota.ResetDate) + "</div>")
		}
		output.WriteString("</div>")
	}
}

func renderCursorStatus(output *bytes.Buffer, raw json.RawMessage, chinese bool) {
	var payload struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(raw, &payload)
	if payload.Status != "" {
		output.WriteString("<div class=\"notice\">" + html.EscapeString(payload.Status) + "</div>")
	}
	message := "Cursor Agent CLI does not expose quota fields, so remaining requests cannot be shown reliably."
	link := "Open Cursor's official Usage page"
	if chinese {
		message = "Cursor Agent CLI 不提供额度字段，因此此处不能可靠展示剩余请求数。"
		link = "打开 Cursor 官方 Usage 页面"
	}
	output.WriteString("<p class=\"notice\">" + message + "<br><a href=\"https://cursor.com/dashboard?tab=usage\" target=\"_blank\" rel=\"noreferrer\">" + link + "</a></p>")
}
