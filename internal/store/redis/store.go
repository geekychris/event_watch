// Package redis is the Redis-backed store implementation.
//
// Key layout:
//   ew:stream:<topic>   Stream. XADD on Append; XRANGE for backfill.
//                       Each entry carries a single "e" field with the
//                       JSON-encoded core.Event.
//   ew:state:<topic>    String. Current computed state as JSON.
//   ew:meta:<topic>     Hash: object_type, ttl_seconds, created_at,
//                       last_event_at, last_seq (all as strings / unix ns).
//   ew:seq:<topic>      Integer counter used to assign monotonic Seq.
//   ew:topics           Set of live topic names, for enumeration + sweeps.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/chris/event_watch/internal/core"
	"github.com/chris/event_watch/internal/store"
	"github.com/redis/go-redis/v9"
)

type Options struct {
	Addr     string
	Password string
	DB       int
}

type Store struct {
	c *redis.Client
}

func New(ctx context.Context, opt Options) (*Store, error) {
	c := redis.NewClient(&redis.Options{Addr: opt.Addr, Password: opt.Password, DB: opt.DB})
	if err := c.Ping(ctx).Err(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return &Store{c: c}, nil
}

func (s *Store) Close() error { return s.c.Close() }

func streamKey(topic string) string { return "ew:stream:" + topic }
func stateKey(topic string) string  { return "ew:state:" + topic }
func metaKey(topic string) string   { return "ew:meta:" + topic }
func seqKey(topic string) string    { return "ew:seq:" + topic }

const topicsSet = "ew:topics"

func (s *Store) Append(ctx context.Context, e *core.Event) (uint64, error) {
	// INCR seq before we serialize so it can be embedded.
	seq, err := s.c.Incr(ctx, seqKey(e.Topic)).Uint64()
	if err != nil {
		return 0, err
	}
	e.Seq = seq
	if e.ID == "" {
		e.ID = core.NewID()
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now()
	}
	body, err := json.Marshal(e)
	if err != nil {
		return 0, err
	}

	ot, _ := core.ParseObjectType(e.Topic)
	pipe := s.c.TxPipeline()
	pipe.XAdd(ctx, &redis.XAddArgs{
		Stream: streamKey(e.Topic),
		ID:     "*",
		Values: map[string]any{"e": body, "seq": seq},
	})
	pipe.HSet(ctx, metaKey(e.Topic), map[string]any{
		"object_type":   ot,
		"last_seq":      seq,
		"last_event_at": e.OccurredAt.UnixNano(),
	})
	pipe.HSetNX(ctx, metaKey(e.Topic), "created_at", time.Now().UnixNano())
	pipe.SAdd(ctx, topicsSet, e.Topic)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}
	return seq, nil
}

func (s *Store) Read(ctx context.Context, topic string, fromSeq uint64, limit int) ([]*core.Event, error) {
	// We don't index events by seq on the Redis side; instead we walk the
	// stream and filter by embedded seq. Streams are chronological and the
	// seq is monotonic, so we can stop early once fromSeq is reached.
	entries, err := s.c.XRange(ctx, streamKey(topic), "-", "+").Result()
	if err != nil {
		return nil, err
	}
	out := make([]*core.Event, 0, len(entries))
	for _, en := range entries {
		e, err := decodeEntry(en)
		if err != nil {
			continue
		}
		if e.Seq < fromSeq {
			continue
		}
		out = append(out, e)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (s *Store) Latest(ctx context.Context, topic string, n int) ([]*core.Event, error) {
	if n <= 0 {
		return nil, nil
	}
	entries, err := s.c.XRevRangeN(ctx, streamKey(topic), "+", "-", int64(n)).Result()
	if err != nil {
		return nil, err
	}
	// XRevRangeN is newest-first; reverse to keep the store contract (oldest first).
	out := make([]*core.Event, 0, len(entries))
	for i := len(entries) - 1; i >= 0; i-- {
		e, err := decodeEntry(entries[i])
		if err != nil {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

func decodeEntry(en redis.XMessage) (*core.Event, error) {
	body, ok := en.Values["e"].(string)
	if !ok {
		return nil, errors.New("stream entry missing 'e'")
	}
	var e core.Event
	if err := json.Unmarshal([]byte(body), &e); err != nil {
		return nil, err
	}
	return &e, nil
}

func (s *Store) GetState(ctx context.Context, topic string) (json.RawMessage, error) {
	v, err := s.c.Get(ctx, stateKey(topic)).Bytes()
	if err == redis.Nil {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return v, nil
}

func (s *Store) SetState(ctx context.Context, topic string, state json.RawMessage) error {
	return s.c.Set(ctx, stateKey(topic), []byte(state), 0).Err()
}

func (s *Store) GetMeta(ctx context.Context, topic string) (*core.TopicMeta, error) {
	m, err := s.c.HGetAll(ctx, metaKey(topic)).Result()
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, store.ErrNotFound
	}
	tm := &core.TopicMeta{Topic: topic, ObjectType: m["object_type"]}
	if v, err := strconv.ParseUint(m["last_seq"], 10, 64); err == nil {
		tm.LastSeq = v
	}
	if v, err := strconv.ParseInt(m["last_event_at"], 10, 64); err == nil {
		tm.LastEventAt = time.Unix(0, v)
	}
	if v, err := strconv.ParseInt(m["created_at"], 10, 64); err == nil {
		tm.CreatedAt = time.Unix(0, v)
	}
	if v, err := strconv.ParseInt(m["ttl_seconds"], 10, 64); err == nil && v > 0 {
		tm.TTL = time.Duration(v) * time.Second
	}
	return tm, nil
}

func (s *Store) UpsertMeta(ctx context.Context, meta *core.TopicMeta) error {
	fields := map[string]any{}
	if meta.ObjectType != "" {
		fields["object_type"] = meta.ObjectType
	}
	if meta.TTL > 0 {
		fields["ttl_seconds"] = int64(meta.TTL / time.Second)
	}
	if len(fields) == 0 {
		return nil
	}
	return s.c.HSet(ctx, metaKey(meta.Topic), fields).Err()
}

func (s *Store) ListTopics(ctx context.Context, prefix string, limit int) ([]string, error) {
	// SMEMBERS is fine at v1 scale (< 100k topics). SSCAN + filtering is the
	// path for larger deployments.
	members, err := s.c.SMembers(ctx, topicsSet).Result()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(members))
	for _, m := range members {
		if prefix == "" || (len(m) >= len(prefix) && m[:len(prefix)] == prefix) {
			out = append(out, m)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *Store) Expire(ctx context.Context, cutoff time.Time) (int, error) {
	topics, err := s.ListTopics(ctx, "", 0)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, t := range topics {
		meta, err := s.GetMeta(ctx, t)
		if err != nil {
			continue
		}
		if !meta.LastEventAt.IsZero() && meta.LastEventAt.Before(cutoff) {
			if err := s.DeleteTopic(ctx, t); err == nil {
				n++
			}
		}
	}
	return n, nil
}

func (s *Store) DeleteTopic(ctx context.Context, topic string) error {
	pipe := s.c.TxPipeline()
	pipe.Del(ctx, streamKey(topic), stateKey(topic), metaKey(topic), seqKey(topic))
	pipe.SRem(ctx, topicsSet, topic)
	_, err := pipe.Exec(ctx)
	return err
}
