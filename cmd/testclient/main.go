// eventwatch-cli is a scriptable harness for the event_watch server. It
// exists to exercise every code path from a shell without touching a browser.
//
//   subscribe:  connect + subscribe, print events until Ctrl-C
//   publish:    send one event and exit
//   simulate:   fire a canned lifecycle for a topic (pr, build, deploy, job, chat)
//   fanout:     load test — N subscribers on one topic + a publisher at R hz
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"time"

	"github.com/chris/event_watch/client"
	"github.com/chris/event_watch/internal/core"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]
	switch cmd {
	case "subscribe":
		runSubscribe(args)
	case "publish":
		runPublish(args)
	case "simulate":
		runSimulate(args)
	case "fanout":
		runFanout(args)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  eventwatch-cli subscribe --url ws://.../ws --topic <t> [--from latest|last:N|seq:N] [--token T]
  eventwatch-cli publish   --url ws://.../ws --topic <t> --type <ty> [--payload JSON] [--token T]
  eventwatch-cli simulate  --url ws://.../ws --kind pr|build|deploy|job|chat [--topic <t>] [--token T]
  eventwatch-cli fanout    --url ws://.../ws --topic <t> --subs N --rate HZ --duration DUR`)
}

func mustDial(url, token string) *client.Client {
	c, err := client.Dial(context.Background(), url, client.WithAuthToken(token))
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	return c
}

func parseFrom(s string) core.From {
	switch {
	case s == "" || s == "latest":
		return core.Latest()
	case strings.HasPrefix(s, "last:"):
		var n uint64
		fmt.Sscanf(s[5:], "%d", &n)
		return core.LastN(n)
	case strings.HasPrefix(s, "seq:"):
		var n uint64
		fmt.Sscanf(s[4:], "%d", &n)
		return core.Seq(n)
	}
	return core.Latest()
}

// -- subscribe --

func runSubscribe(args []string) {
	fs := flag.NewFlagSet("subscribe", flag.ExitOnError)
	url := fs.String("url", "ws://localhost:8080/ws", "server ws URL")
	token := fs.String("token", "", "bearer token")
	topic := fs.String("topic", "", "topic to subscribe")
	from := fs.String("from", "latest", "latest|last:N|seq:N")
	_ = fs.Parse(args)
	if *topic == "" {
		log.Fatal("--topic required")
	}
	c := mustDial(*url, *token)
	defer c.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	h, err := c.Subscribe(ctx, *topic, parseFrom(*from), func(e *core.Event) {
		b, _ := json.Marshal(e)
		fmt.Println(string(b))
	})
	if err != nil {
		log.Fatal(err)
	}
	defer h.Close()

	<-ctx.Done()
}

// -- publish --

func runPublish(args []string) {
	fs := flag.NewFlagSet("publish", flag.ExitOnError)
	url := fs.String("url", "ws://localhost:8080/ws", "server ws URL")
	token := fs.String("token", "", "bearer token")
	topic := fs.String("topic", "", "topic")
	typ := fs.String("type", "", "event type")
	payload := fs.String("payload", "", "JSON payload")
	_ = fs.Parse(args)
	if *topic == "" || *typ == "" {
		log.Fatal("--topic and --type required")
	}
	c := mustDial(*url, *token)
	defer c.Close()
	seq, err := c.Publish(context.Background(), &core.Event{
		Topic: *topic, Type: *typ, Payload: json.RawMessage(*payload),
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("seq=%d\n", seq)
}

// -- simulate --

type simulateStep struct {
	Type    string
	Payload map[string]any
	Delay   time.Duration
}

var simulations = map[string][]simulateStep{
	"pr": {
		{Type: "pr_opened", Payload: map[string]any{"title": "Add feature", "author": "alice", "base": "main", "head": "abc123"}, Delay: 200 * time.Millisecond},
		{Type: "pr_review_requested", Payload: map[string]any{"reviewer": "bob"}, Delay: 200 * time.Millisecond},
		{Type: "check_run_completed", Payload: map[string]any{"conclusion": "success", "name": "test"}, Delay: 400 * time.Millisecond},
		{Type: "pr_commented", Delay: 200 * time.Millisecond},
		{Type: "pr_reviewed", Payload: map[string]any{"state": "approved"}, Delay: 200 * time.Millisecond},
		{Type: "pr_merged", Delay: 200 * time.Millisecond},
	},
	"build": {
		{Type: "build_queued", Delay: 100 * time.Millisecond},
		{Type: "build_started", Delay: 200 * time.Millisecond},
		{Type: "step_started", Payload: map[string]any{"step": "compile"}, Delay: 200 * time.Millisecond},
		{Type: "step_finished", Payload: map[string]any{"step": "compile", "status": "success"}, Delay: 500 * time.Millisecond},
		{Type: "step_started", Payload: map[string]any{"step": "test"}, Delay: 200 * time.Millisecond},
		{Type: "step_finished", Payload: map[string]any{"step": "test", "status": "success"}, Delay: 500 * time.Millisecond},
		{Type: "build_finished", Payload: map[string]any{"status": "success"}, Delay: 200 * time.Millisecond},
	},
	"deploy": {
		{Type: "deploy_started", Payload: map[string]any{"version": "v42", "env": "prod", "service": "api"}, Delay: 200 * time.Millisecond},
		{Type: "health_check_pass", Delay: 500 * time.Millisecond},
		{Type: "deploy_finished", Payload: map[string]any{"status": "success"}, Delay: 200 * time.Millisecond},
	},
	"job": {
		{Type: "job_started", Payload: map[string]any{"name": "reindex"}, Delay: 100 * time.Millisecond},
		{Type: "job_progress", Payload: map[string]any{"percent": 25}, Delay: 300 * time.Millisecond},
		{Type: "job_log", Payload: map[string]any{"line": "processing shard 1"}, Delay: 100 * time.Millisecond},
		{Type: "job_progress", Payload: map[string]any{"percent": 50}, Delay: 300 * time.Millisecond},
		{Type: "job_log", Payload: map[string]any{"line": "processing shard 2"}, Delay: 100 * time.Millisecond},
		{Type: "job_progress", Payload: map[string]any{"percent": 75}, Delay: 300 * time.Millisecond},
		{Type: "job_progress", Payload: map[string]any{"percent": 100}, Delay: 300 * time.Millisecond},
		{Type: "job_finished", Delay: 200 * time.Millisecond},
	},
	"chat": {
		{Type: "user_joined", Payload: map[string]any{"user": "alice"}, Delay: 100 * time.Millisecond},
		{Type: "user_joined", Payload: map[string]any{"user": "bob"}, Delay: 100 * time.Millisecond},
		{Type: "msg_posted", Payload: map[string]any{"id": "m1", "user": "alice", "text": "hey team"}, Delay: 300 * time.Millisecond},
		{Type: "msg_posted", Payload: map[string]any{"id": "m2", "user": "bob", "text": "hi"}, Delay: 300 * time.Millisecond},
		{Type: "msg_edited", Payload: map[string]any{"id": "m1", "text": "hey team!"}, Delay: 300 * time.Millisecond},
	},
}

var defaultSimTopics = map[string]string{
	"pr":     "pr/octo/hello/1",
	"build":  "build/ci/42",
	"deploy": "deploy/prod/api",
	"job":    "job/reindex-1",
	"chat":   "chat/general",
}

func runSimulate(args []string) {
	fs := flag.NewFlagSet("simulate", flag.ExitOnError)
	url := fs.String("url", "ws://localhost:8080/ws", "server ws URL")
	token := fs.String("token", "", "bearer token")
	kind := fs.String("kind", "", "pr|build|deploy|job|chat")
	topic := fs.String("topic", "", "override default topic")
	_ = fs.Parse(args)

	steps, ok := simulations[*kind]
	if !ok {
		log.Fatalf("unknown --kind %q", *kind)
	}
	tp := *topic
	if tp == "" {
		tp = defaultSimTopics[*kind]
	}
	c := mustDial(*url, *token)
	defer c.Close()

	for _, s := range steps {
		payload, _ := json.Marshal(s.Payload)
		seq, err := c.Publish(context.Background(), &core.Event{Topic: tp, Type: s.Type, Payload: payload})
		if err != nil {
			log.Fatalf("publish %s: %v", s.Type, err)
		}
		fmt.Printf("%s seq=%d %s\n", tp, seq, s.Type)
		time.Sleep(s.Delay)
	}
}

// -- fanout load test --

func runFanout(args []string) {
	fs := flag.NewFlagSet("fanout", flag.ExitOnError)
	url := fs.String("url", "ws://localhost:8080/ws", "server ws URL")
	token := fs.String("token", "", "bearer token")
	topic := fs.String("topic", "chat/load", "topic")
	subs := fs.Int("subs", 100, "number of subscribers")
	rate := fs.Int("rate", 50, "publishes per second")
	duration := fs.Duration("duration", 15*time.Second, "test duration")
	_ = fs.Parse(args)

	// One connection per subscriber-batch to keep sockets bounded.
	perConn := 20
	conns := (*subs + perConn - 1) / perConn
	var received atomic.Int64
	handles := make([]*client.Handle, 0, *subs)
	clients := make([]*client.Client, 0, conns)
	for i := 0; i < conns; i++ {
		c := mustDial(*url, *token)
		clients = append(clients, c)
		n := perConn
		if i == conns-1 {
			n = *subs - i*perConn
		}
		for j := 0; j < n; j++ {
			h, err := c.Subscribe(context.Background(), *topic, core.Latest(), func(*core.Event) { received.Add(1) })
			if err != nil {
				log.Fatal(err)
			}
			handles = append(handles, h)
		}
	}
	defer func() {
		for _, h := range handles {
			h.Close()
		}
		for _, c := range clients {
			c.Close()
		}
	}()

	pub := mustDial(*url, *token)
	defer pub.Close()

	ctx, cancel := context.WithTimeout(context.Background(), *duration)
	defer cancel()
	tick := time.NewTicker(time.Second / time.Duration(*rate))
	defer tick.Stop()
	var sent atomic.Int64
	for {
		select {
		case <-ctx.Done():
			// let last events settle
			time.Sleep(500 * time.Millisecond)
			fmt.Printf("sent=%d received=%d expected=%d subs=%d\n",
				sent.Load(), received.Load(), sent.Load()*int64(*subs), *subs)
			return
		case <-tick.C:
			_, err := pub.Publish(context.Background(), &core.Event{
				Topic: *topic, Type: "msg_posted",
				Payload: json.RawMessage(`{"user":"loadgen","text":"tick"}`),
			})
			if err == nil {
				sent.Add(1)
			}
		}
	}
}
