package github

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func mkReq(event string, body map[string]any) *http.Request {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest("POST", "/", bytes.NewReader(b))
	r.Header.Set("X-GitHub-Event", event)
	return r
}

// -- Verify --

func TestVerify_NoSecret_Accepts(t *testing.T) {
	p := New("")
	r := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(`{}`)))
	if err := p.Verify(r); err != nil {
		t.Fatalf("no-secret should accept: %v", err)
	}
}

func TestVerify_MatchingSignature_BodyRestored(t *testing.T) {
	secret := "s3cr3t"
	body := []byte(`{"hi":"there"}`)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	sig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	p := New(secret)
	r := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	r.Header.Set("X-Hub-Signature-256", sig)
	if err := p.Verify(r); err != nil {
		t.Fatalf("want accept, got %v", err)
	}
	out, _ := io.ReadAll(r.Body)
	if !bytes.Equal(out, body) {
		t.Fatalf("body not restored after Verify: %q vs %q", out, body)
	}
}

func TestVerify_MissingSignature(t *testing.T) {
	p := New("s3cr3t")
	r := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(`{}`)))
	if err := p.Verify(r); err == nil {
		t.Fatal("missing signature should reject")
	}
}

func TestVerify_WrongSignature(t *testing.T) {
	p := New("s3cr3t")
	r := httptest.NewRequest("POST", "/", bytes.NewReader([]byte(`{}`)))
	r.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	if err := p.Verify(r); err == nil {
		t.Fatal("bad signature should reject")
	}
}

// -- Transform --

func TestTransform_PullRequestOpened(t *testing.T) {
	p := New("")
	body := map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number": 42, "title": "hi",
			"user": map[string]any{"login": "alice"},
			"base": map[string]any{"ref": "main"},
			"head": map[string]any{"sha": "abc"},
		},
		"sender":     map[string]any{"login": "alice"},
		"repository": map[string]any{"full_name": "octo/hello"},
	}
	events, err := p.Transform(mkReq("pull_request", body))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "pr_opened" || events[0].Topic != "pr/octo/hello/42" {
		t.Fatalf("bad transform: %+v", events)
	}
}

func TestTransform_PullRequestClosed_Merged(t *testing.T) {
	p := New("")
	body := map[string]any{
		"action": "closed",
		"pull_request": map[string]any{
			"number": 5, "merged": true,
		},
		"sender":     map[string]any{"login": "a"},
		"repository": map[string]any{"full_name": "octo/hello"},
	}
	events, err := p.Transform(mkReq("pull_request", body))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Type != "pr_merged" {
		t.Fatalf("expected pr_merged, got %+v", events)
	}
}

func TestTransform_PullRequestClosed_NotMerged(t *testing.T) {
	p := New("")
	body := map[string]any{
		"action":       "closed",
		"pull_request": map[string]any{"number": 5, "merged": false},
		"sender":       map[string]any{"login": "a"},
		"repository":   map[string]any{"full_name": "octo/hello"},
	}
	events, _ := p.Transform(mkReq("pull_request", body))
	if len(events) != 1 || events[0].Type != "pr_closed" {
		t.Fatalf("expected pr_closed, got %+v", events)
	}
}

func TestTransform_PullRequestLabeled(t *testing.T) {
	p := New("")
	body := map[string]any{
		"action":       "labeled",
		"pull_request": map[string]any{"number": 5},
		"label":        map[string]any{"name": "bug"},
		"sender":       map[string]any{"login": "a"},
		"repository":   map[string]any{"full_name": "octo/hello"},
	}
	events, _ := p.Transform(mkReq("pull_request", body))
	if len(events) != 1 || events[0].Type != "pr_labeled" {
		t.Fatalf("expected pr_labeled, got %+v", events)
	}
	var pl map[string]any
	_ = json.Unmarshal(events[0].Payload, &pl)
	if pl["label"] != "bug" {
		t.Fatalf("label payload wrong: %v", pl)
	}
}

func TestTransform_PullRequestReviewSubmitted(t *testing.T) {
	p := New("")
	body := map[string]any{
		"action":       "submitted",
		"pull_request": map[string]any{"number": 5},
		"review":       map[string]any{"state": "approved"},
		"sender":       map[string]any{"login": "bob"},
		"repository":   map[string]any{"full_name": "octo/hello"},
	}
	events, _ := p.Transform(mkReq("pull_request_review", body))
	if len(events) != 1 || events[0].Type != "pr_reviewed" {
		t.Fatalf("expected pr_reviewed, got %+v", events)
	}
}

func TestTransform_IssueCommentOnPR(t *testing.T) {
	p := New("")
	body := map[string]any{
		"action": "created",
		"issue": map[string]any{
			"number":       9,
			"pull_request": map[string]any{}, // presence marks it as a PR comment
		},
		"sender":     map[string]any{"login": "a"},
		"repository": map[string]any{"full_name": "octo/hello"},
	}
	events, _ := p.Transform(mkReq("issue_comment", body))
	if len(events) != 1 || events[0].Type != "pr_commented" {
		t.Fatalf("expected pr_commented, got %+v", events)
	}
}

func TestTransform_IssueCommentOnPlainIssue_Ignored(t *testing.T) {
	p := New("")
	body := map[string]any{
		"action":     "created",
		"issue":      map[string]any{"number": 9},
		"sender":     map[string]any{"login": "a"},
		"repository": map[string]any{"full_name": "octo/hello"},
	}
	events, _ := p.Transform(mkReq("issue_comment", body))
	if len(events) != 0 {
		t.Fatalf("plain issue comment should not map to a PR event, got %+v", events)
	}
}

func TestTransform_CheckRunCompleted_MultiplePRs(t *testing.T) {
	p := New("")
	body := map[string]any{
		"action": "completed",
		"check_run": map[string]any{
			"conclusion": "success", "name": "ci",
			"pull_requests": []any{
				map[string]any{"number": 1},
				map[string]any{"number": 2},
			},
		},
		"repository": map[string]any{"full_name": "octo/hello"},
	}
	events, err := p.Transform(mkReq("check_run", body))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events (one per PR), got %d", len(events))
	}
	if events[0].Topic != "pr/octo/hello/1" || events[1].Topic != "pr/octo/hello/2" {
		t.Fatalf("bad topics: %v", []string{events[0].Topic, events[1].Topic})
	}
}

func TestTransform_Ping_Accepted(t *testing.T) {
	p := New("")
	events, err := p.Transform(mkReq("ping", map[string]any{
		"repository": map[string]any{"full_name": "octo/hello"},
	}))
	if err != nil {
		t.Fatalf("ping should accept: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("ping should produce zero events, got %d", len(events))
	}
}

func TestTransform_MissingRepository_Errors(t *testing.T) {
	p := New("")
	_, err := p.Transform(mkReq("pull_request", map[string]any{"action": "opened"}))
	if err == nil {
		t.Fatal("expected error when repository is missing")
	}
}

func TestTransform_UnknownEvent_Ignored(t *testing.T) {
	p := New("")
	events, err := p.Transform(mkReq("release", map[string]any{
		"repository": map[string]any{"full_name": "octo/hello"},
	}))
	if err != nil || len(events) != 0 {
		t.Fatalf("unknown event should silently produce zero events; got %+v err=%v", events, err)
	}
}

func TestTransform_RepoFromOwnerLoginFallback(t *testing.T) {
	// Payload uses owner.login + name instead of full_name.
	p := New("")
	body := map[string]any{
		"action":       "opened",
		"pull_request": map[string]any{"number": 1, "title": "x", "user": map[string]any{"login": "a"}, "base": map[string]any{"ref": "main"}, "head": map[string]any{"sha": "d"}},
		"sender":       map[string]any{"login": "a"},
		"repository":   map[string]any{"owner": map[string]any{"login": "octo"}, "name": "hello"},
	}
	events, err := p.Transform(mkReq("pull_request", body))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Topic != "pr/octo/hello/1" {
		t.Fatalf("bad topic: %+v", events)
	}
}
