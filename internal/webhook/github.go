package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func VerifyGitHubSignature(body []byte, signature string, secret string) bool {
	if signature == "" {
		return false
	}

	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	sig, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expectedMAC := mac.Sum(nil)

	return hmac.Equal(sig, expectedMAC)
}

func ParseGitHubEvent(r *http.Request) (event string, branch string, err error) {
	event = r.Header.Get("X-GitHub-Event")
	if event == "" {
		return "", "", fmt.Errorf("missing X-GitHub-Event header")
	}

	// Read body to extract ref from payload
	body, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	ref := extractRefFromPayload(string(body))
	if strings.HasPrefix(ref, "refs/heads/") {
		branch = strings.TrimPrefix(ref, "refs/heads/")
	}

	return event, branch, nil
}

func extractRefFromPayload(body string) string {
	var payload struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return ""
	}
	return payload.Ref
}

func VerifyGitLabToken(r *http.Request, token string) bool {
	got := r.Header.Get("X-Gitlab-Token")
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func VerifyGiteaSignature(body []byte, signature string, secret string) bool {
	return VerifyGitHubSignature(body, signature, secret)
}
