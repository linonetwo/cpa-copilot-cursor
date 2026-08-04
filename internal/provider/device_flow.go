package provider

import (
	"bytes"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

type pendingDeviceFlow struct {
	VerificationURI string
	UserCode        string
	ExpiresAt       time.Time
}

var pendingDeviceFlows = struct {
	sync.Mutex
	items map[string]pendingDeviceFlow
}{items: make(map[string]pendingDeviceFlow)}

func rememberDeviceFlow(kind Kind, state string, metadata map[string]any, expiresAt time.Time) bool {
	if kind != KindCopilot || strings.TrimSpace(state) == "" {
		return false
	}
	verificationURI, _ := metadata["verification_uri"].(string)
	userCode, _ := metadata["user_code"].(string)
	verificationURI = strings.TrimSpace(verificationURI)
	userCode = strings.TrimSpace(userCode)
	if verificationURI == "" || userCode == "" {
		return false
	}
	parsed, err := url.Parse(verificationURI)
	if err != nil || parsed.Scheme != "https" || !strings.EqualFold(parsed.Hostname(), "github.com") {
		return false
	}
	key := deviceFlowKey(kind, state)
	pendingDeviceFlows.Lock()
	pendingDeviceFlows.items[key] = pendingDeviceFlow{
		VerificationURI: verificationURI,
		UserCode:        userCode,
		ExpiresAt:       expiresAt,
	}
	pendingDeviceFlows.Unlock()
	if delay := time.Until(expiresAt); delay > 0 {
		time.AfterFunc(delay, func() {
			pendingDeviceFlows.Lock()
			if current, ok := pendingDeviceFlows.items[key]; ok && current.ExpiresAt.Equal(expiresAt) {
				delete(pendingDeviceFlows.items, key)
			}
			pendingDeviceFlows.Unlock()
		})
	}
	return true
}

func forgetDeviceFlow(kind Kind, state string) {
	pendingDeviceFlows.Lock()
	delete(pendingDeviceFlows.items, deviceFlowKey(kind, state))
	pendingDeviceFlows.Unlock()
}

func lookupDeviceFlow(kind Kind, state string) (pendingDeviceFlow, bool) {
	key := deviceFlowKey(kind, state)
	pendingDeviceFlows.Lock()
	defer pendingDeviceFlows.Unlock()
	flow, ok := pendingDeviceFlows.items[key]
	if !ok {
		return pendingDeviceFlow{}, false
	}
	if !flow.ExpiresAt.IsZero() && time.Now().After(flow.ExpiresAt) {
		delete(pendingDeviceFlows.items, key)
		return pendingDeviceFlow{}, false
	}
	return flow, true
}

func deviceFlowKey(kind Kind, state string) string {
	return string(kind) + "\x00" + state
}

func deviceFlowURL(kind Kind, state string) string {
	path := "/v0/resource/plugins/" + pluginID(kind) + "/device"
	query := url.Values{"state": []string{state}}.Encode()
	baseURL := strings.TrimSpace(os.Getenv("CPA_COPILOT_CURSOR_PUBLIC_BASE_URL"))
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return path + "?" + query
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + path
	parsed.RawQuery = query
	return parsed.String()
}

func handleDeviceFlow(kind Kind, request pluginapi.ManagementRequest) ([]byte, error) {
	state := strings.TrimSpace(request.Query.Get("state"))
	flow, ok := lookupDeviceFlow(kind, state)
	if !ok {
		return okEnvelope(htmlResponse(http.StatusNotFound, renderDeviceFlowError(isChinese(request.Headers))))
	}
	return okEnvelope(htmlResponse(http.StatusOK, renderDeviceFlowPage(flow, isChinese(request.Headers))))
}

func isChinese(headers http.Header) bool {
	return strings.Contains(strings.ToLower(headers.Get("Accept-Language")), "zh")
}

func renderDeviceFlowError(chinese bool) []byte {
	title := "Login expired"
	message := "This GitHub Copilot login is unavailable or expired. Return to CPA Manager Plus and start again."
	if chinese {
		title = "登录已失效"
		message = "此次 GitHub Copilot 登录不存在或已过期。请返回 CPA Manager Plus 重新发起登录。"
	}
	return renderDeviceFlowDocument(title, "<section class=\"card\"><h1>"+html.EscapeString(title)+"</h1><p>"+html.EscapeString(message)+"</p></section>", chinese)
}

func renderDeviceFlowPage(flow pendingDeviceFlow, chinese bool) []byte {
	title := "Authorize GitHub Copilot"
	introduction := "Copy the device code, then open GitHub and paste it. Keep this page open so the code remains visible."
	codeLabel := "Device code"
	copyCode := "Copy device code"
	copyLink := "Copy verification link"
	openLink := "Open GitHub verification"
	returnHint := "After authorization, return to CPA Manager Plus. The original OAuth page will detect completion automatically."
	copied := "Copied"
	if chinese {
		title = "授权 GitHub Copilot"
		introduction = "先复制设备码，再打开 GitHub 并粘贴。请保留此页，以便随时查看设备码。"
		codeLabel = "设备码"
		copyCode = "复制设备码"
		copyLink = "复制验证链接"
		openLink = "打开 GitHub 验证页"
		returnHint = "完成授权后返回 CPA Manager Plus，原 OAuth 页面会自动检测登录结果。"
		copied = "已复制"
	}

	var body bytes.Buffer
	body.WriteString("<section class=\"card\">")
	body.WriteString("<div class=\"brand\">GitHub Copilot Subscription</div>")
	body.WriteString("<h1>" + html.EscapeString(title) + "</h1>")
	body.WriteString("<p>" + html.EscapeString(introduction) + "</p>")
	body.WriteString("<div class=\"label\">" + html.EscapeString(codeLabel) + "</div>")
	body.WriteString("<div class=\"code\" id=\"device-code\">" + html.EscapeString(flow.UserCode) + "</div>")
	body.WriteString("<div class=\"actions\">")
	body.WriteString(copyButton(copyCode, flow.UserCode, copied))
	body.WriteString(copyButton(copyLink, flow.VerificationURI, copied))
	body.WriteString("<a class=\"button primary\" href=\"" + html.EscapeString(flow.VerificationURI) + "\" target=\"_blank\" rel=\"noopener noreferrer\">" + html.EscapeString(openLink) + "</a>")
	body.WriteString("</div>")
	body.WriteString("<p class=\"hint\">" + html.EscapeString(returnHint) + "</p>")
	body.WriteString("</section>")
	return renderDeviceFlowDocument(title, body.String(), chinese)
}

func copyButton(label, value, copied string) string {
	return "<button class=\"button\" type=\"button\" data-copy=\"" + html.EscapeString(value) + "\" data-copied=\"" + html.EscapeString(copied) + "\">" + html.EscapeString(label) + "</button>"
}

func renderDeviceFlowDocument(title, body string, chinese bool) []byte {
	language := "en"
	if chinese {
		language = "zh-CN"
	}
	return []byte(fmt.Sprintf(`<!doctype html>
<html lang="%s">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <meta name="referrer" content="no-referrer">
  <title>%s</title>
  <style>
    :root{color-scheme:light dark;font-family:ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
    *{box-sizing:border-box}
    body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0b1020;color:#e5e7eb;padding:24px}
    .card{width:min(100%%,640px);background:#151b2e;border:1px solid #2b3553;border-radius:18px;padding:28px;box-shadow:0 20px 60px #0006}
    .brand,.label,.hint{color:#9ca3af}.brand{font-weight:700}.label{margin-top:24px;font-size:.9rem}
    h1{margin:8px 0 12px;font-size:clamp(1.8rem,5vw,2.5rem)}p{line-height:1.65}
    .code{margin-top:8px;padding:20px;border:1px solid #4b5f91;border-radius:14px;background:#0b1020;text-align:center;font:700 clamp(2rem,9vw,3.5rem)/1 ui-monospace,SFMono-Regular,Consolas,monospace;letter-spacing:.08em}
    .actions{display:flex;flex-wrap:wrap;gap:10px;margin-top:20px}.button{appearance:none;border:1px solid #50618d;border-radius:10px;background:#232d49;color:#f9fafb;padding:11px 15px;font:inherit;font-weight:650;text-decoration:none;cursor:pointer}
    .button:hover{background:#2c395d}.button.primary{background:#2563eb;border-color:#3b82f6}.button.primary:hover{background:#1d4ed8}.hint{margin:20px 0 0;font-size:.92rem}
  </style>
</head>
<body>
  <main>%s</main>
  <script>
    document.addEventListener("click",async event=>{
      const button=event.target.closest("[data-copy]");
      if(!button)return;
      const label=button.textContent;
      try{await navigator.clipboard.writeText(button.dataset.copy);button.textContent=button.dataset.copied;setTimeout(()=>button.textContent=label,1500)}
      catch{window.prompt("Copy",button.dataset.copy)}
    });
  </script>
</body>
</html>`, language, html.EscapeString(title), body))
}
