package objtypes

import (
	"encoding/json"
	"testing"
	"time"
)

func TestStringField_SetAndDelete(t *testing.T) {
	raw := fold(t, StringReducer{}, "str/name",
		ev("", "str_set", map[string]any{"value": "alice"}),
	)
	var s StringFieldState
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.Value != "alice" || !s.Exists {
		t.Fatalf("after set: %+v", s)
	}
	raw = fold(t, StringReducer{}, "str/name",
		ev("", "str_set", map[string]any{"value": "alice"}),
		ev("", "str_delete", nil),
	)
	_ = json.Unmarshal(raw, &s)
	if s.Value != "" || s.Exists {
		t.Fatalf("after delete: %+v", s)
	}
}

func TestIntField_SetIncrDecr(t *testing.T) {
	raw := fold(t, IntReducer{}, "int/counter",
		ev("", "int_set", map[string]any{"value": 10}),
		ev("", "int_incr", map[string]any{"delta": 3}),
		ev("", "int_incr", nil), // default +1
		ev("", "int_decr", map[string]any{"delta": 4}),
		ev("", "int_decr", nil), // default -1
	)
	var s IntFieldState
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	// 10 + 3 + 1 - 4 - 1 = 9
	if s.Value != 9 || !s.Exists {
		t.Fatalf("after arithmetic: %+v", s)
	}
}

func TestIntField_IncrOnEmpty_StartsFromZero(t *testing.T) {
	// No prior set — incr should treat missing value as 0 and exists → true.
	raw := fold(t, IntReducer{}, "int/hits",
		ev("", "int_incr", map[string]any{"delta": 5}),
	)
	var s IntFieldState
	_ = json.Unmarshal(raw, &s)
	if s.Value != 5 || !s.Exists {
		t.Fatalf("incr-from-empty: %+v", s)
	}
}

func TestIntField_DeleteResets(t *testing.T) {
	raw := fold(t, IntReducer{}, "int/x",
		ev("", "int_set", map[string]any{"value": 100}),
		ev("", "int_delete", nil),
	)
	var s IntFieldState
	_ = json.Unmarshal(raw, &s)
	if s.Value != 0 || s.Exists {
		t.Fatalf("after delete: %+v", s)
	}
}

func TestTimeField_SetAndNow(t *testing.T) {
	when := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	raw := fold(t, TimeReducer{}, "time/last_seen",
		ev("", "time_set", map[string]any{"value": when.Format(time.RFC3339Nano)}),
	)
	var s TimeFieldState
	_ = json.Unmarshal(raw, &s)
	if !s.Exists || !s.Value.Equal(when) {
		t.Fatalf("after set: %+v", s)
	}
	// time_now uses the event's OccurredAt, which fold() stamps.
	e := ev("", "time_now", nil)
	raw2 := fold(t, TimeReducer{}, "time/last_seen", e)
	_ = json.Unmarshal(raw2, &s)
	if !s.Exists || s.Value.IsZero() {
		t.Fatalf("after now: %+v", s)
	}
}

func TestTimeField_Add(t *testing.T) {
	when := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	raw := fold(t, TimeReducer{}, "time/x",
		ev("", "time_set", map[string]any{"value": when.Format(time.RFC3339Nano)}),
		ev("", "time_add", map[string]any{"seconds": 3600}), // +1h
		ev("", "time_add", map[string]any{"seconds": -60}),  // -1m
	)
	var s TimeFieldState
	_ = json.Unmarshal(raw, &s)
	want := when.Add(3600*time.Second - 60*time.Second)
	if !s.Value.Equal(want) {
		t.Fatalf("after add: got %v, want %v", s.Value, want)
	}
}

func TestTimeField_Delete(t *testing.T) {
	when := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	raw := fold(t, TimeReducer{}, "time/x",
		ev("", "time_set", map[string]any{"value": when.Format(time.RFC3339Nano)}),
		ev("", "time_delete", nil),
	)
	var s TimeFieldState
	_ = json.Unmarshal(raw, &s)
	if s.Exists || !s.Value.IsZero() {
		t.Fatalf("after delete: %+v", s)
	}
}
