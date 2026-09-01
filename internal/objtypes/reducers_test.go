package objtypes

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/chris/event_watch/internal/computed"
	"github.com/chris/event_watch/internal/core"
)

func ev(topic, typ string, payload any) *core.Event {
	p, _ := json.Marshal(payload)
	return &core.Event{Topic: topic, Type: typ, OccurredAt: time.Now(), Payload: p}
}

func fold(t *testing.T, r computed.Reducer, topic string, events ...*core.Event) json.RawMessage {
	var state json.RawMessage
	var err error
	for _, e := range events {
		e.Topic = topic
		state, err = r.Apply(state, e)
		if err != nil {
			t.Fatalf("Apply(%s): %v", e.Type, err)
		}
	}
	return state
}

func TestPRLifecycle(t *testing.T) {
	raw := fold(t, PRReducer{}, "pr/o/r/1",
		ev("", "pr_opened", map[string]any{"title": "T", "author": "alice", "base": "main", "head": "f"}),
		ev("", "pr_review_requested", map[string]any{"reviewer": "bob"}),
		ev("", "pr_reviewed", map[string]any{"state": "approved"}),
		ev("", "check_run_completed", map[string]any{"conclusion": "success"}),
		ev("", "check_run_completed", map[string]any{"conclusion": "failure"}),
		ev("", "pr_commented", nil),
		ev("", "pr_merged", nil),
	)
	var s PRState
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.Title != "T" || s.Author != "alice" || s.State != "merged" {
		t.Fatalf("bad final state: %+v", s)
	}
	if s.Approvals != 1 || s.Checks.Passed != 1 || s.Checks.Failed != 1 || s.Comments != 1 {
		t.Fatalf("counters wrong: %+v", s)
	}
	if len(s.Reviewers) != 1 || s.Reviewers[0] != "bob" {
		t.Fatalf("reviewers wrong: %+v", s.Reviewers)
	}
}

func TestBuildLifecycle(t *testing.T) {
	raw := fold(t, BuildReducer{}, "build/ci/1",
		ev("", "build_queued", nil),
		ev("", "build_started", nil),
		ev("", "step_started", map[string]any{"step": "test"}),
		ev("", "step_finished", map[string]any{"step": "test", "status": "success"}),
		ev("", "build_finished", map[string]any{"status": "success"}),
	)
	var s BuildState
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.Status != "success" {
		t.Fatalf("status=%q, want success", s.Status)
	}
	if len(s.Steps) != 1 || s.Steps[0].Status != "success" {
		t.Fatalf("steps wrong: %+v", s.Steps)
	}
}

func TestDeployRollback(t *testing.T) {
	raw := fold(t, DeployReducer{}, "deploy/prod/api",
		ev("", "deploy_started", map[string]any{"version": "v1", "env": "prod", "service": "api"}),
		ev("", "deploy_finished", map[string]any{"status": "success"}),
		ev("", "deploy_started", map[string]any{"version": "v2"}),
		ev("", "health_check_fail", nil),
		ev("", "rollback", nil),
	)
	var s DeployState
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.CurrentVersion != "v1" || s.PreviousVersion != "v2" {
		t.Fatalf("versions wrong after rollback: cur=%q prev=%q", s.CurrentVersion, s.PreviousVersion)
	}
	if s.InProgress {
		t.Fatalf("in_progress should be false after rollback")
	}
}

func TestJobProgressAndLogsCap(t *testing.T) {
	events := []*core.Event{
		ev("", "job_started", map[string]any{"name": "reindex"}),
		ev("", "job_progress", map[string]any{"percent": 25}),
	}
	// Push more logs than the cap.
	for i := 0; i < jobLogCap+10; i++ {
		events = append(events, ev("", "job_log", map[string]any{"line": "hello"}))
	}
	events = append(events, ev("", "job_finished", nil))
	raw := fold(t, JobReducer{}, "job/x", events...)
	var s JobState
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.Status != "succeeded" || s.Percent != 100 {
		t.Fatalf("final state wrong: %+v", s)
	}
	if len(s.Logs) != jobLogCap {
		t.Fatalf("log cap not enforced: %d", len(s.Logs))
	}
}

func TestChatEditAndDelete(t *testing.T) {
	raw := fold(t, ChatReducer{}, "chat/room",
		ev("", "user_joined", map[string]any{"user": "alice"}),
		ev("", "user_joined", map[string]any{"user": "bob"}),
		ev("", "msg_posted", map[string]any{"id": "m1", "user": "alice", "text": "hi"}),
		ev("", "msg_posted", map[string]any{"id": "m2", "user": "bob", "text": "hey"}),
		ev("", "msg_edited", map[string]any{"id": "m1", "text": "hello"}),
		ev("", "msg_deleted", map[string]any{"id": "m2"}),
		ev("", "user_left", map[string]any{"user": "bob"}),
	)
	var s ChatState
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Participants) != 1 || s.Participants[0] != "alice" {
		t.Fatalf("participants wrong: %+v", s.Participants)
	}
	if len(s.Recent) != 1 || s.Recent[0].Text != "hello" || !s.Recent[0].Edited {
		t.Fatalf("recent wrong: %+v", s.Recent)
	}
}

func TestRegistryApplyDispatchesByObjectType(t *testing.T) {
	reg := computed.NewRegistry()
	reg.Register(PRReducer{}, BuildReducer{}, DeployReducer{}, JobReducer{}, ChatReducer{})
	e := &core.Event{Topic: "pr/o/r/1", Type: "pr_opened", OccurredAt: time.Now(),
		Payload: json.RawMessage(`{"title":"hi"}`)}
	got, err := reg.Apply(nil, e)
	if err != nil {
		t.Fatal(err)
	}
	var s PRState
	if err := json.Unmarshal(got, &s); err != nil {
		t.Fatal(err)
	}
	if s.Title != "hi" {
		t.Fatalf("title=%q, want hi", s.Title)
	}
}
