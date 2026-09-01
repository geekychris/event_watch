package objtypes

import (
	"encoding/json"
	"time"

	"github.com/chris/event_watch/internal/core"
)

type PRChecks struct {
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Pending int `json:"pending"`
}

type PRState struct {
	Title            string    `json:"title,omitempty"`
	Author           string    `json:"author,omitempty"`
	Base             string    `json:"base,omitempty"`
	Head             string    `json:"head,omitempty"`
	State            string    `json:"state,omitempty"` // open|closed|merged
	Reviewers        []string  `json:"reviewers,omitempty"`
	Approvals        int       `json:"approvals"`
	ChangesRequested int       `json:"changes_requested"`
	Labels           []string  `json:"labels,omitempty"`
	Checks           PRChecks  `json:"checks"`
	Comments         int       `json:"comments"`
	MergedAt         time.Time `json:"merged_at,omitempty"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type PRReducer struct{}

func (PRReducer) ObjectType() string { return "pr" }

func (PRReducer) Apply(raw json.RawMessage, e *core.Event) (json.RawMessage, error) {
	var s PRState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &s); err != nil {
			return raw, err
		}
	}
	if s.State == "" {
		s.State = "open"
	}
	s.UpdatedAt = e.OccurredAt

	// Payload fields are looked up loosely so upstream can send whichever
	// subset makes sense for the event type.
	p := map[string]any{}
	if len(e.Payload) > 0 {
		_ = json.Unmarshal(e.Payload, &p)
	}
	getStr := func(k string) string { v, _ := p[k].(string); return v }

	switch e.Type {
	case "pr_opened":
		if v := getStr("title"); v != "" {
			s.Title = v
		}
		if v := getStr("author"); v != "" {
			s.Author = v
		}
		if v := getStr("base"); v != "" {
			s.Base = v
		}
		if v := getStr("head"); v != "" {
			s.Head = v
		}
		s.State = "open"
	case "pr_sync":
		if v := getStr("head"); v != "" {
			s.Head = v
		}
		// A new push resets pending checks; leaving pass/fail counts alone
		// here so the reducer stays event-driven — check_run_completed will
		// update them.
	case "pr_review_requested":
		if v := getStr("reviewer"); v != "" {
			s.Reviewers = addUnique(s.Reviewers, v)
		}
	case "pr_reviewed":
		switch getStr("state") {
		case "approved":
			s.Approvals++
		case "changes_requested":
			s.ChangesRequested++
		}
	case "pr_commented":
		s.Comments++
	case "pr_labeled":
		if v := getStr("label"); v != "" {
			s.Labels = addUnique(s.Labels, v)
		}
	case "pr_unlabeled":
		if v := getStr("label"); v != "" {
			s.Labels = removeString(s.Labels, v)
		}
	case "pr_merged":
		s.State = "merged"
		s.MergedAt = e.OccurredAt
	case "pr_closed":
		if s.State != "merged" {
			s.State = "closed"
		}
	case "check_run_completed":
		switch getStr("conclusion") {
		case "success":
			s.Checks.Passed++
			if s.Checks.Pending > 0 {
				s.Checks.Pending--
			}
		case "failure", "timed_out", "cancelled":
			s.Checks.Failed++
			if s.Checks.Pending > 0 {
				s.Checks.Pending--
			}
		case "pending", "queued", "in_progress":
			s.Checks.Pending++
		}
	}

	return json.Marshal(s)
}

func addUnique(xs []string, v string) []string {
	for _, x := range xs {
		if x == v {
			return xs
		}
	}
	return append(xs, v)
}

func removeString(xs []string, v string) []string {
	out := xs[:0]
	for _, x := range xs {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
