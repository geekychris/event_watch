package core

// FromKind selects how a new subscription is seeded before live events start
// streaming.
type FromKind int

const (
	FromLatest FromKind = iota // only new events (default)
	FromLastN                  // replay last N historical events
	FromSeq                    // replay everything with Seq >= Value
)

type From struct {
	Kind  FromKind
	Value uint64 // last-N count for FromLastN, seq for FromSeq
}

func Latest() From        { return From{Kind: FromLatest} }
func LastN(n uint64) From { return From{Kind: FromLastN, Value: n} }
func Seq(s uint64) From   { return From{Kind: FromSeq, Value: s} }
