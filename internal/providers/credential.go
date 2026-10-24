package providers

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Credential is the part of a CPA credential file the fetchers need.
type Credential struct {
	AccessToken string
	AccountID   string
	// UserID is the xAI account subject, sent as x-userid.
	UserID   string
	ProxyURL string
}

// ParseCredential reads the access token, account and user IDs and proxy URL
// from a CPA credential file.
func ParseCredential(raw json.RawMessage) Credential {
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return Credential{}
	}
	metadata, _ := data["metadata"].(map[string]any)
	attributes, _ := data["attributes"].(map[string]any)
	userID := firstString(data, metadata, "sub", "subject", "user_id", "userId")
	if userID == "" {
		userID = firstString(attributes, nil, "sub", "subject", "user_id", "userId")
	}
	return Credential{
		AccessToken: firstString(data, metadata, "access_token", "accessToken"),
		AccountID:   firstString(data, metadata, "account_id", "chatgpt_account_id", "chatgptAccountId"),
		UserID:      userID,
		ProxyURL:    firstString(data, nil, "proxy_url", "proxyUrl"),
	}
}

func firstString(primary, secondary map[string]any, keys ...string) string {
	for _, source := range []map[string]any{primary, secondary} {
		for _, key := range keys {
			if value := scalarString(source[key]); value != "" {
				return value
			}
		}
	}
	return ""
}

func scalarString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case json.Number:
		return v.String()
	}
	return ""
}
