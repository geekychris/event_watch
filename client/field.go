package client

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Field helpers are thin sugar over Publish + GetState. Nothing here uses
// server routes that don't already exist — a field mutation is just a
// publish and a value fetch is just a get_state.

type StringField struct {
	c     *Client
	Topic string
}

func (c *Client) StringField(topic string) *StringField { return &StringField{c: c, Topic: topic} }

func (f *StringField) Set(ctx context.Context, value string) (uint64, error) {
	return f.c.Publish(ctx, &Event{Topic: f.Topic, Type: "str_set",
		Payload: mustPayload(map[string]any{"value": value})})
}

func (f *StringField) Delete(ctx context.Context) (uint64, error) {
	return f.c.Publish(ctx, &Event{Topic: f.Topic, Type: "str_delete"})
}

// Get returns (value, exists, error). exists=false is not an error — it just
// means the field has never been set (or was deleted).
func (f *StringField) Get(ctx context.Context) (string, bool, error) {
	raw, err := f.c.GetState(ctx, f.Topic)
	if err != nil {
		return "", false, err
	}
	if len(raw) == 0 {
		return "", false, nil
	}
	var s struct {
		Value  string `json:"value"`
		Exists bool   `json:"exists"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", false, err
	}
	return s.Value, s.Exists, nil
}

type IntField struct {
	c     *Client
	Topic string
}

func (c *Client) IntField(topic string) *IntField { return &IntField{c: c, Topic: topic} }

func (f *IntField) Set(ctx context.Context, value int64) (uint64, error) {
	return f.c.Publish(ctx, &Event{Topic: f.Topic, Type: "int_set",
		Payload: mustPayload(map[string]any{"value": value})})
}

func (f *IntField) Incr(ctx context.Context, delta int64) (uint64, error) {
	return f.c.Publish(ctx, &Event{Topic: f.Topic, Type: "int_incr",
		Payload: mustPayload(map[string]any{"delta": delta})})
}

func (f *IntField) Decr(ctx context.Context, delta int64) (uint64, error) {
	return f.c.Publish(ctx, &Event{Topic: f.Topic, Type: "int_decr",
		Payload: mustPayload(map[string]any{"delta": delta})})
}

func (f *IntField) Delete(ctx context.Context) (uint64, error) {
	return f.c.Publish(ctx, &Event{Topic: f.Topic, Type: "int_delete"})
}

func (f *IntField) Get(ctx context.Context) (int64, bool, error) {
	raw, err := f.c.GetState(ctx, f.Topic)
	if err != nil {
		return 0, false, err
	}
	if len(raw) == 0 {
		return 0, false, nil
	}
	var s struct {
		Value  int64 `json:"value"`
		Exists bool  `json:"exists"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, false, err
	}
	return s.Value, s.Exists, nil
}

type TimeField struct {
	c     *Client
	Topic string
}

func (c *Client) TimeField(topic string) *TimeField { return &TimeField{c: c, Topic: topic} }

func (f *TimeField) Set(ctx context.Context, t time.Time) (uint64, error) {
	return f.c.Publish(ctx, &Event{Topic: f.Topic, Type: "time_set",
		Payload: mustPayload(map[string]any{"value": t.UTC().Format(time.RFC3339Nano)})})
}

// Now records the server's current time (server-side, ignores clock skew).
func (f *TimeField) Now(ctx context.Context) (uint64, error) {
	return f.c.Publish(ctx, &Event{Topic: f.Topic, Type: "time_now"})
}

func (f *TimeField) Add(ctx context.Context, d time.Duration) (uint64, error) {
	return f.c.Publish(ctx, &Event{Topic: f.Topic, Type: "time_add",
		Payload: mustPayload(map[string]any{"seconds": int64(d / time.Second)})})
}

func (f *TimeField) Delete(ctx context.Context) (uint64, error) {
	return f.c.Publish(ctx, &Event{Topic: f.Topic, Type: "time_delete"})
}

func (f *TimeField) Get(ctx context.Context) (time.Time, bool, error) {
	raw, err := f.c.GetState(ctx, f.Topic)
	if err != nil {
		return time.Time{}, false, err
	}
	if len(raw) == 0 {
		return time.Time{}, false, nil
	}
	var s struct {
		Value  time.Time `json:"value"`
		Exists bool      `json:"exists"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return time.Time{}, false, err
	}
	return s.Value, s.Exists, nil
}

func mustPayload(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Errorf("field payload marshal: %w", err))
	}
	return b
}
