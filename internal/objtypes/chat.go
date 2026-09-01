package objtypes

import (
	"encoding/json"
	"time"

	"github.com/chris/event_watch/internal/core"
)

const chatMsgCap = 50

type ChatMessage struct {
	ID     string    `json:"id"`
	User   string    `json:"user"`
	Text   string    `json:"text"`
	PostedAt time.Time `json:"posted_at"`
	Edited bool      `json:"edited,omitempty"`
}

type ChatState struct {
	Room         string        `json:"room,omitempty"`
	Participants []string      `json:"participants,omitempty"`
	Recent       []ChatMessage `json:"recent,omitempty"`
}

type ChatReducer struct{}

func (ChatReducer) ObjectType() string { return "chat" }

func (ChatReducer) Apply(raw json.RawMessage, e *core.Event) (json.RawMessage, error) {
	var s ChatState
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

	switch e.Type {
	case "user_joined":
		if u := getStr("user"); u != "" {
			s.Participants = addUnique(s.Participants, u)
		}
	case "user_left":
		if u := getStr("user"); u != "" {
			s.Participants = removeString(s.Participants, u)
		}
	case "msg_posted":
		id := getStr("id")
		if id == "" {
			id = e.ID
		}
		s.Recent = append(s.Recent, ChatMessage{
			ID: id, User: getStr("user"), Text: getStr("text"), PostedAt: e.OccurredAt,
		})
		if len(s.Recent) > chatMsgCap {
			s.Recent = s.Recent[len(s.Recent)-chatMsgCap:]
		}
	case "msg_edited":
		id := getStr("id")
		text := getStr("text")
		for i := range s.Recent {
			if s.Recent[i].ID == id {
				s.Recent[i].Text = text
				s.Recent[i].Edited = true
				break
			}
		}
	case "msg_deleted":
		id := getStr("id")
		out := s.Recent[:0]
		for _, m := range s.Recent {
			if m.ID != id {
				out = append(out, m)
			}
		}
		s.Recent = out
	}
	return json.Marshal(s)
}
