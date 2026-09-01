package objtypes

import (
	"encoding/json"
	"time"

	"github.com/chris/event_watch/internal/core"
)

// Field reducers.
//
// "Fields" are scalar primitives (string, int, time) that live in the same
// topic namespace as event-object types (pr/build/deploy/...). Every mutation
// is an event on the topic; the reducer folds those events into a single
// snapshot that represents the current value.
//
// Because the broker serialises Append→Reduce→Publish per event, operations
// like Incr are naturally atomic under concurrent publishers: no need for a
// separate compare-and-swap primitive.

// -- string field --

type StringFieldState struct {
	Value     string    `json:"value"`
	Exists    bool      `json:"exists"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type StringReducer struct{}

func (StringReducer) ObjectType() string { return "str" }

func (StringReducer) Apply(raw json.RawMessage, e *core.Event) (json.RawMessage, error) {
	var s StringFieldState
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	s.UpdatedAt = e.OccurredAt
	p := map[string]any{}
	if len(e.Payload) > 0 {
		_ = json.Unmarshal(e.Payload, &p)
	}
	switch e.Type {
	case "str_set":
		if v, ok := p["value"].(string); ok {
			s.Value = v
			s.Exists = true
		}
	case "str_delete":
		s.Value = ""
		s.Exists = false
	}
	return json.Marshal(s)
}

// -- int field --

type IntFieldState struct {
	Value     int64     `json:"value"`
	Exists    bool      `json:"exists"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type IntReducer struct{}

func (IntReducer) ObjectType() string { return "int" }

func (IntReducer) Apply(raw json.RawMessage, e *core.Event) (json.RawMessage, error) {
	var s IntFieldState
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	s.UpdatedAt = e.OccurredAt
	p := map[string]any{}
	if len(e.Payload) > 0 {
		_ = json.Unmarshal(e.Payload, &p)
	}
	switch e.Type {
	case "int_set":
		if v, ok := numberAsInt64(p["value"]); ok {
			s.Value = v
			s.Exists = true
		}
	case "int_incr":
		delta := int64(1)
		if v, ok := numberAsInt64(p["delta"]); ok {
			delta = v
		}
		s.Value += delta
		s.Exists = true
	case "int_decr":
		delta := int64(1)
		if v, ok := numberAsInt64(p["delta"]); ok {
			delta = v
		}
		s.Value -= delta
		s.Exists = true
	case "int_delete":
		s.Value = 0
		s.Exists = false
	}
	return json.Marshal(s)
}

// -- time field --

type TimeFieldState struct {
	Value     time.Time `json:"value,omitempty"`
	Exists    bool      `json:"exists"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

type TimeReducer struct{}

func (TimeReducer) ObjectType() string { return "time" }

func (TimeReducer) Apply(raw json.RawMessage, e *core.Event) (json.RawMessage, error) {
	var s TimeFieldState
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &s)
	}
	s.UpdatedAt = e.OccurredAt
	p := map[string]any{}
	if len(e.Payload) > 0 {
		_ = json.Unmarshal(e.Payload, &p)
	}
	switch e.Type {
	case "time_set":
		if v, ok := p["value"].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				s.Value = t
				s.Exists = true
			}
		}
	case "time_now":
		s.Value = e.OccurredAt
		s.Exists = true
	case "time_add":
		if v, ok := numberAsInt64(p["seconds"]); ok {
			s.Value = s.Value.Add(time.Duration(v) * time.Second)
			s.Exists = true
		}
	case "time_delete":
		s.Value = time.Time{}
		s.Exists = false
	}
	return json.Marshal(s)
}

// numberAsInt64 accepts JSON's float64 (the default number type) or a raw
// int64 and returns an int64.
func numberAsInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case int64:
		return x, true
	case int:
		return int64(x), true
	}
	return 0, false
}
