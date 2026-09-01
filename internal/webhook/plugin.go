// Package webhook is the plugin surface for external providers pushing raw
// payloads into the event system. Each plugin owns signature verification
// and payload → event translation for one provider.
package webhook

import (
	"net/http"
	"sync"

	"github.com/chris/event_watch/internal/core"
)

type WebhookPlugin interface {
	Name() string
	Verify(*http.Request) error
	Transform(*http.Request) ([]*core.Event, error)
}

type Registry struct {
	mu      sync.RWMutex
	plugins map[string]WebhookPlugin
}

func NewRegistry() *Registry { return &Registry{plugins: map[string]WebhookPlugin{}} }

func (r *Registry) Register(p WebhookPlugin) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins[p.Name()] = p
}

func (r *Registry) Get(name string) (WebhookPlugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.plugins[name]
	return p, ok
}

func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.plugins))
	for n := range r.plugins {
		out = append(out, n)
	}
	return out
}
