package objtypes

import (
	"encoding/json"
	"time"

	"github.com/chris/event_watch/internal/core"
)

const jobLogCap = 50

type JobLog struct {
	At   time.Time `json:"at"`
	Line string    `json:"line"`
}

type JobState struct {
	Name       string    `json:"name,omitempty"`
	Percent    int       `json:"percent"`
	ETASeconds int       `json:"eta_seconds,omitempty"`
	Status     string    `json:"status"` // running|succeeded|failed
	Logs       []JobLog  `json:"logs,omitempty"`
	StartedAt  time.Time `json:"started_at,omitempty"`
	FinishedAt time.Time `json:"finished_at,omitempty"`
}

type JobReducer struct{}

func (JobReducer) ObjectType() string { return "job" }

func (JobReducer) Apply(raw json.RawMessage, e *core.Event) (json.RawMessage, error) {
	var s JobState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &s); err != nil {
			return raw, err
		}
	}
	p := map[string]any{}
	if len(e.Payload) > 0 {
		_ = json.Unmarshal(e.Payload, &p)
	}
	getStr := func(k string) string { v, _ := p[k].(string); return v }
	getInt := func(k string) int {
		switch v := p[k].(type) {
		case float64:
			return int(v)
		case int:
			return v
		}
		return 0
	}

	switch e.Type {
	case "job_started":
		s.Status = "running"
		s.StartedAt = e.OccurredAt
		if v := getStr("name"); v != "" {
			s.Name = v
		}
	case "job_progress":
		if v := getInt("percent"); v > 0 {
			s.Percent = v
		}
		if v := getInt("eta_seconds"); v > 0 {
			s.ETASeconds = v
		}
	case "job_log":
		s.Logs = append(s.Logs, JobLog{At: e.OccurredAt, Line: getStr("line")})
		if len(s.Logs) > jobLogCap {
			s.Logs = s.Logs[len(s.Logs)-jobLogCap:]
		}
	case "job_finished":
		s.Status = "succeeded"
		s.Percent = 100
		s.FinishedAt = e.OccurredAt
	case "job_failed":
		s.Status = "failed"
		s.FinishedAt = e.OccurredAt
	}
	return json.Marshal(s)
}
