// Package computed maintains a "deep state" snapshot for each topic by folding
// its event stream through a per-object-type Reducer. State is stored as
// json.RawMessage so each reducer marshals its own struct.
package computed

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/chris/event_watch/internal/core"
)

var ErrUnknownType = errors.New("no reducer registered for object type")

// Reducer folds events for one object type. Apply receives the current state
// (may be nil for a fresh topic) and the incoming event, and returns the new
// state.
type Reducer interface {
	ObjectType() string
	Apply(state json.RawMessage, e *core.Event) (json.RawMessage, error)
}

type Registry struct {
	mu    sync.RWMutex
	byOT  map[string]Reducer
}

func NewRegistry() *Registry { return &Registry{byOT: map[string]Reducer{}} }

func (r *Registry) Register(reducers ...Reducer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, red := range reducers {
		r.byOT[red.ObjectType()] = red
	}
}

func (r *Registry) For(objectType string) (Reducer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	red, ok := r.byOT[objectType]
	if !ok {
		return nil, ErrUnknownType
	}
	return red, nil
}

// Apply looks up the reducer for e.Topic's object type, applies it, and
// returns the new state. If no reducer is registered the state is unchanged.
func (r *Registry) Apply(state json.RawMessage, e *core.Event) (json.RawMessage, error) {
	ot, err := core.ParseObjectType(e.Topic)
	if err != nil {
		return state, err
	}
	red, err := r.For(ot)
	if err != nil {
		// no reducer registered — this is fine; the raw stream is still stored.
		return state, nil
	}
	return red.Apply(state, e)
}
