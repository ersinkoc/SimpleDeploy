package webhook

import (
	"encoding/json"
	"net/url"
	"strings"
)

// pushInfo is what the deploy decision needs from a push payload.
type pushInfo struct {
	// Ref is the pushed ref ("refs/heads/main", "refs/tags/v1"), empty if the
	// payload carried none.
	Ref string
	// Deleted reports that the push DELETED the ref. GitHub sends this as an
	// explicit flag; every provider also signals it by an all-zero `after`
	// object id, which is the git protocol's own convention.
	Deleted bool
	// Parsed reports whether the payload could be read at all. False means the
	// deploy decision has no information, which callers must treat as a refusal
	// rather than as "no branch restriction".
	Parsed bool
}

// extractPushInfo reads a push payload in either delivery mode GitHub offers:
// application/json (the body IS the JSON document) and
// application/x-www-form-urlencoded (the body is payload=<urlencoded JSON>).
//
// The form mode used to be unreadable here, which left the ref empty — and an
// empty ref skipped the branch filter entirely, so a repository configured with
// that content type redeployed on every push to every branch.
//
// The HMAC is unaffected by any of this: providers sign the raw body, and
// verification runs on the raw body before this is called.
func extractPushInfo(body string) pushInfo {
	var payload struct {
		Ref     string `json:"ref"`
		After   string `json:"after"`
		Deleted *bool  `json:"deleted"`
	}

	decode := func(s string) bool {
		return json.Unmarshal([]byte(s), &payload) == nil
	}

	parsed := decode(body)
	if !parsed {
		if vals, err := url.ParseQuery(body); err == nil {
			if p := vals.Get("payload"); p != "" {
				parsed = decode(p)
			}
		}
	}
	if !parsed {
		return pushInfo{}
	}

	deleted := false
	if payload.Deleted != nil {
		deleted = *payload.Deleted
	}
	// All-zero object id means the ref was deleted. Checked in addition to the
	// flag because GitLab and Gitea do not send `deleted` at all, and it is the
	// git wire protocol's own way of saying "this ref is gone".
	if payload.After != "" && strings.Trim(payload.After, "0") == "" {
		deleted = true
	}

	return pushInfo{Ref: payload.Ref, Deleted: deleted, Parsed: true}
}
