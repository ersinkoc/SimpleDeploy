package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
)

// provider describes how a webhook delivery from a Git hosting provider is
// authenticated and identified. Each provider's quirks (bare-hex Gitea
// signatures, form-encoded GitHub payloads, GitLab's plain-token comparison)
// live in one struct with its own focused tests, making the handler a simple
// table walk and adding a new provider (Bitbucket, Forgejo) a data change.
type provider struct {
	name      string
	detect    func(h http.Header) bool
	verify    func(h http.Header, body []byte, secret string) bool
	eventName func(h http.Header) string
}

// providers is the ordered list of supported webhook providers. The order
// matters only for the detect check: GitHub is first because modern Gitea
// sends a GitHub-compatible X-Hub-Signature-256 header alongside its own
// X-Gitea-Signature, and GitHub's stricter sha256= prefix check is the
// preferred verification path when both are present.
var providers = []provider{
	{
		name: "github",
		detect: func(h http.Header) bool {
			return h.Get("X-Hub-Signature-256") != ""
		},
		verify: func(h http.Header, body []byte, secret string) bool {
			return VerifyGitHubSignature(body, h.Get("X-Hub-Signature-256"), secret)
		},
		eventName: func(h http.Header) string {
			return h.Get("X-GitHub-Event")
		},
	},
	{
		name: "gitlab",
		detect: func(h http.Header) bool {
			return h.Get("X-Gitlab-Token") != ""
		},
		verify: func(h http.Header, _ []byte, secret string) bool {
			got := h.Get("X-Gitlab-Token")
			return subtle.ConstantTimeCompare([]byte(got), []byte(secret)) == 1
		},
		eventName: func(h http.Header) string {
			return h.Get("X-Gitlab-Event")
		},
	},
	{
		name: "gitea",
		detect: func(h http.Header) bool {
			return h.Get("X-Gitea-Signature") != ""
		},
		verify: func(h http.Header, body []byte, secret string) bool {
			return VerifyGiteaSignature(body, h.Get("X-Gitea-Signature"), secret)
		},
		eventName: func(h http.Header) string {
			return h.Get("X-Gitea-Event")
		},
	},
}

// detectProvider returns the first provider whose detect function matches the
// request headers, or nil if no provider is recognised. Returns the provider
// even when no secret is configured: the caller uses the event header from
// the matched provider regardless of whether verification was performed.
func detectProvider(h http.Header) *provider {
	for i := range providers {
		if providers[i].detect(h) {
			return &providers[i]
		}
	}
	return nil
}

// VerifyGitHubSignature verifies GitHub's X-Hub-Signature-256 header, which is
// "sha256=<hex-hmac>".
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
