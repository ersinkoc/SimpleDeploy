package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
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

// extractRefFromPayload pulls the "ref" field out of a push payload. It
// understands both delivery modes GitHub offers: application/json (the body IS
// the JSON document) and application/x-www-form-urlencoded (the body is
// payload=<urlencoded JSON>). The form mode used to be unparseable here, which
// left ref empty — and an empty ref skips the branch filter entirely, so a
// repository configured with the form content type redeployed on every push to
// every branch. The HMAC is unaffected either way: providers sign the raw
// body, and verification runs on the raw body before this is called.
func extractRefFromPayload(body string) string {
	var payload struct {
		Ref string `json:"ref"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err == nil {
		return payload.Ref
	}
	if vals, err := url.ParseQuery(body); err == nil {
		if p := vals.Get("payload"); p != "" {
			if err := json.Unmarshal([]byte(p), &payload); err == nil {
				return payload.Ref
			}
		}
	}
	return ""
}

func VerifyGitLabToken(r *http.Request, token string) bool {
	got := r.Header.Get("X-Gitlab-Token")
	return subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

// VerifyGiteaSignature verifies Gitea's X-Gitea-Signature header. Unlike
// GitHub's X-Hub-Signature-256, Gitea puts the BARE hex HMAC-SHA256 digest in
// this header — no "sha256=" prefix (services/webhook/deliver.go in Gitea
// hex-encodes the digest straight into X-Gitea-Signature; the prefixed form
// goes into its separate X-Hub-Signature-256 compatibility header). Delegating
// to VerifyGitHubSignature, as this used to, therefore rejected every genuine
// Gitea signature; deploys only worked at all because modern Gitea also sends
// the GitHub-compat header, which the server checks first. The prefixed form
// is still tolerated so a sender using it is not broken.
func VerifyGiteaSignature(body []byte, signature string, secret string) bool {
	sig, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil || len(sig) == 0 {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hmac.Equal(sig, mac.Sum(nil))
}
