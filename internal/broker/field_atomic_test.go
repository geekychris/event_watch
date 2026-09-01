package broker

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/chris/event_watch/internal/computed"
	"github.com/chris/event_watch/internal/core"
	"github.com/chris/event_watch/internal/hub"
	"github.com/chris/event_watch/internal/objtypes"
	memstore "github.com/chris/event_watch/internal/store/memory"
)

// TestIntIncr_AtomicUnderConcurrency: N goroutines each fire int_incr with
// delta=1 concurrently. The final state must equal N (no lost updates).
// Ordering is provided by the broker's per-event serialisation lock, which
// is what makes this a correctness guarantee, not a probabilistic assertion.
func TestIntIncr_AtomicUnderConcurrency(t *testing.T) {
	reg := computed.NewRegistry()
	reg.Register(objtypes.IntReducer{})
	b := New(memstore.New(), hub.New(), reg, time.Hour)

	const N = 500
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := b.Publish(context.Background(), &core.Event{
				Topic: "int/hits", Type: "int_incr",
				Payload: json.RawMessage(`{"delta":1}`),
			}, "test")
			if err != nil {
				t.Errorf("publish: %v", err)
			}
		}()
	}
	wg.Wait()

	state, err := b.GetState(context.Background(), "int/hits")
	if err != nil {
		t.Fatal(err)
	}
	var s objtypes.IntFieldState
	if err := json.Unmarshal(state, &s); err != nil {
		t.Fatal(err)
	}
	if s.Value != int64(N) {
		t.Fatalf("expected value=%d after %d concurrent incrs, got %d", N, N, s.Value)
	}
}
