package client

import "github.com/chris/event_watch/internal/core"

// These aliases let external modules (e.g. the Wails desktop app) reach the
// domain types without importing internal/. The client library is the public
// surface; internal/core is not.
type (
	Event = core.Event
	From  = core.From
)

func Latest() From        { return core.Latest() }
func LastN(n uint64) From { return core.LastN(n) }
func Seq(s uint64) From   { return core.Seq(s) }
