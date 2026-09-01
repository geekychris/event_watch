// Package github implements the webhook.WebhookPlugin for GitHub PR-related
// events. Payload fields are read loosely — we grab the minimum needed to
// map onto pr/<owner>/<repo>/<number> topics.
package github

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/chris/event_watch/internal/core"
)

type Plugin struct {
	secret string
}

func New(secret string) *Plugin { return &Plugin{secret: secret} }

func (p *Plugin) Name() string { return "github" }

// Verify checks X-Hub-Signature-256 against the shared secret. When no
// secret is configured verification is a no-op (dev convenience — publish
// works but you accept unauthenticated webhooks).
func (p *Plugin) Verify(r *http.Request) error {
	if p.secret == "" {
		return nil
	}
	sig := r.Header.Get("X-Hub-Signature-256")
	if sig == "" {
		return errors.New("missing X-Hub-Signature-256")
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		return err
	}
	// The reader is drained — put it back so Transform can re-read.
	r.Body = io.NopCloser(bytes.NewReader(body))

	mac := hmac.New(sha256.New, []byte(p.secret))
	mac.Write(body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return errors.New("signature mismatch")
	}
	return nil
}

func (p *Plugin) Transform(r *http.Request) ([]*core.Event, error) {
	event := r.Header.Get("X-GitHub-Event")
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	// Derive owner/repo from `repository.full_name` or `repository.name` +
	// `repository.owner.login`.
	owner, repo := repoFrom(raw)
	if owner == "" || repo == "" {
		return nil, errors.New("payload missing repository info")
	}

	switch event {
	case "pull_request":
		return prEvent(raw, owner, repo)
	case "pull_request_review":
		return prReviewEvent(raw, owner, repo)
	case "issue_comment":
		return issueCommentEvent(raw, owner, repo)
	case "check_run":
		return checkRunEvent(raw, owner, repo)
	case "ping":
		return nil, nil // GitHub sanity ping — accept with no events
	}
	return nil, nil // unknown event: accept silently
}

// -- helpers --

func repoFrom(raw map[string]any) (string, string) {
	repo, _ := raw["repository"].(map[string]any)
	if repo == nil {
		return "", ""
	}
	if v, ok := repo["full_name"].(string); ok {
		for i := 0; i < len(v); i++ {
			if v[i] == '/' {
				return v[:i], v[i+1:]
			}
		}
	}
	owner, _ := repo["owner"].(map[string]any)
	name, _ := repo["name"].(string)
	if owner != nil {
		if login, ok := owner["login"].(string); ok {
			return login, name
		}
	}
	return "", name
}

func prNumberFrom(raw map[string]any, key string) int64 {
	pr, _ := raw[key].(map[string]any)
	if pr == nil {
		return 0
	}
	switch v := pr["number"].(type) {
	case float64:
		return int64(v)
	}
	return 0
}

func topicFor(owner, repo string, num int64) string {
	return fmt.Sprintf("pr/%s/%s/%d", owner, repo, num)
}

func prEvent(raw map[string]any, owner, repo string) ([]*core.Event, error) {
	action, _ := raw["action"].(string)
	num := prNumberFrom(raw, "pull_request")
	if num == 0 {
		return nil, errors.New("missing pull_request.number")
	}
	pr, _ := raw["pull_request"].(map[string]any)
	sender, _ := raw["sender"].(map[string]any)
	actor := stringField(sender, "login")

	base := ""
	head := ""
	if b, ok := pr["base"].(map[string]any); ok {
		base = stringField(b, "ref")
	}
	if h, ok := pr["head"].(map[string]any); ok {
		head = stringField(h, "sha")
	}
	title := stringField(pr, "title")
	author := ""
	if u, ok := pr["user"].(map[string]any); ok {
		author = stringField(u, "login")
	}
	topic := topicFor(owner, repo, num)

	switch action {
	case "opened", "reopened":
		return []*core.Event{{
			Topic: topic, Type: "pr_opened", Actor: actor,
			Payload: mustMarshal(map[string]any{"title": title, "author": author, "base": base, "head": head}),
		}}, nil
	case "synchronize":
		return []*core.Event{{
			Topic: topic, Type: "pr_sync", Actor: actor,
			Payload: mustMarshal(map[string]any{"head": head}),
		}}, nil
	case "review_requested":
		reviewer := ""
		if rr, ok := raw["requested_reviewer"].(map[string]any); ok {
			reviewer = stringField(rr, "login")
		}
		return []*core.Event{{
			Topic: topic, Type: "pr_review_requested", Actor: actor,
			Payload: mustMarshal(map[string]any{"reviewer": reviewer}),
		}}, nil
	case "labeled":
		label := ""
		if l, ok := raw["label"].(map[string]any); ok {
			label = stringField(l, "name")
		}
		return []*core.Event{{
			Topic: topic, Type: "pr_labeled", Actor: actor,
			Payload: mustMarshal(map[string]any{"label": label}),
		}}, nil
	case "unlabeled":
		label := ""
		if l, ok := raw["label"].(map[string]any); ok {
			label = stringField(l, "name")
		}
		return []*core.Event{{
			Topic: topic, Type: "pr_unlabeled", Actor: actor,
			Payload: mustMarshal(map[string]any{"label": label}),
		}}, nil
	case "closed":
		if merged, _ := pr["merged"].(bool); merged {
			return []*core.Event{{Topic: topic, Type: "pr_merged", Actor: actor}}, nil
		}
		return []*core.Event{{Topic: topic, Type: "pr_closed", Actor: actor}}, nil
	}
	return nil, nil
}

func prReviewEvent(raw map[string]any, owner, repo string) ([]*core.Event, error) {
	action, _ := raw["action"].(string)
	if action != "submitted" {
		return nil, nil
	}
	num := prNumberFrom(raw, "pull_request")
	if num == 0 {
		return nil, nil
	}
	sender, _ := raw["sender"].(map[string]any)
	review, _ := raw["review"].(map[string]any)
	state := stringField(review, "state")
	return []*core.Event{{
		Topic: topicFor(owner, repo, num), Type: "pr_reviewed",
		Actor: stringField(sender, "login"),
		Payload: mustMarshal(map[string]any{"state": state}),
	}}, nil
}

func issueCommentEvent(raw map[string]any, owner, repo string) ([]*core.Event, error) {
	action, _ := raw["action"].(string)
	if action != "created" {
		return nil, nil
	}
	issue, _ := raw["issue"].(map[string]any)
	if issue == nil {
		return nil, nil
	}
	// Only care about PR comments.
	if _, ok := issue["pull_request"]; !ok {
		return nil, nil
	}
	num := int64(0)
	if v, ok := issue["number"].(float64); ok {
		num = int64(v)
	}
	sender, _ := raw["sender"].(map[string]any)
	return []*core.Event{{
		Topic: topicFor(owner, repo, num), Type: "pr_commented",
		Actor: stringField(sender, "login"),
	}}, nil
}

func checkRunEvent(raw map[string]any, owner, repo string) ([]*core.Event, error) {
	action, _ := raw["action"].(string)
	if action != "completed" {
		return nil, nil
	}
	cr, _ := raw["check_run"].(map[string]any)
	if cr == nil {
		return nil, nil
	}
	conclusion := stringField(cr, "conclusion")
	// A check_run may relate to multiple PRs.
	prs, _ := cr["pull_requests"].([]any)
	out := make([]*core.Event, 0, len(prs))
	for _, x := range prs {
		pr, _ := x.(map[string]any)
		var num int64
		if v, ok := pr["number"].(float64); ok {
			num = int64(v)
		}
		if num == 0 {
			continue
		}
		out = append(out, &core.Event{
			Topic: topicFor(owner, repo, num), Type: "check_run_completed",
			Payload: mustMarshal(map[string]any{"conclusion": conclusion, "name": stringField(cr, "name")}),
		})
	}
	return out, nil
}

func stringField(m map[string]any, k string) string {
	if m == nil {
		return ""
	}
	s, _ := m[k].(string)
	return s
}

func mustMarshal(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

