package bridge

import (
	"net/url"
	"strings"
)

func authorizationURL(provider, verificationURI, userCode string) string {
	if provider != "copilot" || userCode == "" {
		return verificationURI
	}
	parsed, err := url.Parse(verificationURI)
	if err != nil {
		return verificationURI
	}
	query := parsed.Query()
	query.Set("user_code", userCode)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func loginMetadata(verificationURI, userCode, instructions string) map[string]any {
	metadata := map[string]any{
		"instructions":     instructions,
		"verification_uri": verificationURI,
	}
	if strings.TrimSpace(userCode) != "" {
		metadata["user_code"] = userCode
	}
	return metadata
}
