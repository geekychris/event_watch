package objtypes

import (
	"encoding/json"
	"time"

	"github.com/chris/event_watch/internal/core"
)

type BuildStep struct {
	Name       string `json:"name"`
	Status     string `json:"status"` // running|success|failed|skipped
	DurationMs int64  `json:"duration_ms,omitempty"`
	startedAt  time.Time
}

type BuildState struct {
	Status       string       `json:"status"` // queued|running|success|failed
	CurrentStep  string       `json:"current_step,omitempty"`
	Steps        []BuildStep  `json:"steps,omitempty"`
	StartedAt    time.Time    `json:"started_at,omitempty"`
	FinishedAt   time.Time    `json:"finished_at,omitempty"`
	DurationMs   int64        `json:"duration_ms,omitempty"`
	stepStarts   map[string]time.Time
}

// A tiny custom marshaller is unnecessary here; the unexported startedAt in
// BuildStep is deliberately dropped from JSON. To keep per-step start times
// across events we shadow them in BuildState.stepStarts (also unexported).
// We serialise those out via a JSON-side struct.

type buildStateWire struct {
	Status      string      `json:"status"`
	CurrentStep string      `json:"current_step,omitempty"`
	Steps       []BuildStep `json:"steps,omitempty"`
	StartedAt   time.Time   `json:"started_at,omitempty"`
	FinishedAt  time.Time   `json:"finished_at,omitempty"`
	DurationMs  int64       `json:"duration_ms,omitempty"`
	StepStarts  map[string]time.Time `json:"_step_starts,omitempty"`
}

func (s BuildState) MarshalJSON() ([]byte, error) {
	return json.Marshal(buildStateWire{
		Status:      s.Status,
		CurrentStep: s.CurrentStep,
		Steps:       s.Steps,
		StartedAt:   s.StartedAt,
		FinishedAt:  s.FinishedAt,
		DurationMs:  s.DurationMs,
		StepStarts:  s.stepStarts,
	})
}

func (s *BuildState) UnmarshalJSON(b []byte) error {
	var w buildStateWire
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	s.Status = w.Status
	s.CurrentStep = w.CurrentStep
	s.Steps = w.Steps
	s.StartedAt = w.StartedAt
	s.FinishedAt = w.FinishedAt
	s.DurationMs = w.DurationMs
	s.stepStarts = w.StepStarts
	return nil
}

type BuildReducer struct{}

func (BuildReducer) ObjectType() string { return "build" }

func (BuildReducer) Apply(raw json.RawMessage, e *core.Event) (json.RawMessage, error) {
	var s BuildState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &s); err != nil {
			return raw, err
		}
	}
	if s.stepStarts == nil {
		s.stepStarts = map[string]time.Time{}
	}
	p := map[string]any{}
	if len(e.Payload) > 0 {
		_ = json.Unmarshal(e.Payload, &p)
	}
	getStr := func(k string) string { v, _ := p[k].(string); return v }

	switch e.Type {
	case "build_queued":
		s.Status = "queued"
	case "build_started":
		s.Status = "running"
		s.StartedAt = e.OccurredAt
	case "step_started":
		name := getStr("step")
		s.CurrentStep = name
		s.stepStarts[name] = e.OccurredAt
		s.Steps = append(s.Steps, BuildStep{Name: name, Status: "running"})
	case "step_finished":
		name := getStr("step")
		status := getStr("status")
		if status == "" {
			status = "success"
		}
		var dur int64
		if t, ok := s.stepStarts[name]; ok {
			dur = e.OccurredAt.Sub(t).Milliseconds()
			delete(s.stepStarts, name)
		}
		for i := range s.Steps {
			if s.Steps[i].Name == name && s.Steps[i].Status == "running" {
				s.Steps[i].Status = status
				s.Steps[i].DurationMs = dur
				break
			}
		}
		if s.CurrentStep == name {
			s.CurrentStep = ""
		}
	case "build_finished":
		status := getStr("status")
		if status == "" {
			status = "success"
		}
		s.Status = status
		s.FinishedAt = e.OccurredAt
		if !s.StartedAt.IsZero() {
			s.DurationMs = s.FinishedAt.Sub(s.StartedAt).Milliseconds()
		}
	}
	return json.Marshal(s)
}
