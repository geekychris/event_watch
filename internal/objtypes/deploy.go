package objtypes

import (
	"encoding/json"
	"time"

	"github.com/chris/event_watch/internal/core"
)

type DeployState struct {
	Env             string    `json:"env,omitempty"`
	Service         string    `json:"service,omitempty"`
	CurrentVersion  string    `json:"current_version,omitempty"`
	PreviousVersion string    `json:"previous_version,omitempty"`
	Health          string    `json:"health"` // healthy|degraded|down|unknown
	InProgress      bool      `json:"in_progress"`
	LastDeployAt    time.Time `json:"last_deploy_at,omitempty"`
	LastSuccessAt   time.Time `json:"last_success_at,omitempty"`
}

type DeployReducer struct{}

func (DeployReducer) ObjectType() string { return "deploy" }

func (DeployReducer) Apply(raw json.RawMessage, e *core.Event) (json.RawMessage, error) {
	var s DeployState
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &s); err != nil {
			return raw, err
		}
	}
	if s.Health == "" {
		s.Health = "unknown"
	}
	p := map[string]any{}
	if len(e.Payload) > 0 {
		_ = json.Unmarshal(e.Payload, &p)
	}
	getStr := func(k string) string { v, _ := p[k].(string); return v }

	switch e.Type {
	case "deploy_started":
		s.InProgress = true
		s.LastDeployAt = e.OccurredAt
		if v := getStr("version"); v != "" {
			s.PreviousVersion = s.CurrentVersion
			s.CurrentVersion = v
		}
		if v := getStr("env"); v != "" {
			s.Env = v
		}
		if v := getStr("service"); v != "" {
			s.Service = v
		}
	case "health_check_pass":
		s.Health = "healthy"
	case "health_check_fail":
		if s.Health == "healthy" {
			s.Health = "degraded"
		} else {
			s.Health = "down"
		}
	case "rollback":
		s.InProgress = false
		s.CurrentVersion, s.PreviousVersion = s.PreviousVersion, s.CurrentVersion
		if v := getStr("to"); v != "" {
			s.CurrentVersion = v
		}
	case "deploy_finished":
		s.InProgress = false
		if getStr("status") == "success" || getStr("status") == "" {
			s.LastSuccessAt = e.OccurredAt
			if s.Health == "" || s.Health == "unknown" {
				s.Health = "healthy"
			}
		}
	}
	return json.Marshal(s)
}
