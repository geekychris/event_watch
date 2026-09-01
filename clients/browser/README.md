# @eventwatch/browser

Browser (and Node 22+) client for the event_watch pub/sub service. Uses
the native `WebSocket` — no build step, no dependencies. Same API surface
as the Go / Python / Java / Rust clients: connect, subscribe (refcounted
local dispatch), publish, get_state, typed field helpers with atomic
`incr` / `decr` on `int/…` topics.

## Install

**From a bundler / Node project:**

```bash
npm install @eventwatch/browser
# or, from a local checkout:
npm install file:./clients/browser
```

**From a plain HTML page** (ES modules — no build tool required):

```html
<script type="module">
  import { Client } from '/path/to/eventwatch/src/index.js';
  const c = await Client.dial('ws://localhost:8080/ws');
  // ...
</script>
```

You can also serve the library from your own static host, a CDN like
esm.sh (`import { Client } from 'https://esm.sh/@eventwatch/browser'`),
or the event_watch server itself (add it under `web/static/`).

## Hello world

```js
import { Client } from '@eventwatch/browser';

const c = await Client.dial('ws://localhost:8080/ws');

// subscribe — Handle.close() removes THIS callback; N callbacks per topic
// share ONE upstream subscription (refcounted).
const h = c.subscribe('int/counter', (ev) => {
  console.log(ev.type, 'seq', ev.seq, 'value now', ev.state?.value);
});

// typed field arithmetic
const counter = c.intField('int/counter');
await counter.set(100);
await counter.incr(5);          // → 105
await counter.decr(3);          // → 102
const [value, exists] = await counter.get();
console.log(value, exists);     // 102 true

h.close();
c.close();
```

## With auth

```js
const c = await Client.dial('ws://localhost:8080/ws', { token: 's3cr3t' });
// Sent as ?access_token=… on the URL — browsers can't set headers on WebSocket.
```

## Subscribe backfill

```js
c.subscribe('chat/room', cb);                          // only new events
c.subscribe('chat/room', cb, { from: 'latest' });      // same
c.subscribe('chat/room', cb, { from: 'last:50' });     // last 50 historical + live
c.subscribe('chat/room', cb, { from: 'seq:100' });     // everything from seq 100 + live
```

## Typed field helpers

```js
// str / int / time — thin wrappers over publish() + getState()
await c.stringField('str/status').set('ok');
await c.stringField('str/status').delete();
const [s, exists] = await c.stringField('str/status').get();

await c.intField('int/hits').incr();            // +1
await c.intField('int/hits').incr(5);           // +5
await c.intField('int/hits').decr(2);           // -2
await c.intField('int/hits').set(0);            // absolute
const [n, exists] = await c.intField('int/hits').get();

await c.timeField('time/last').now();           // server timestamp
await c.timeField('time/last').set(new Date());
await c.timeField('time/last').add(3600);       // +1h
const [when, exists] = await c.timeField('time/last').get();  // when is a Date
```

## Reconnect + resume

Automatic. On WS drop the client:
1. Rejects any in-flight `publish` / `getState` promises with `connection lost`
2. Backoffs exponentially (100ms → 30s cap)
3. Redials, then re-issues each active subscription with `from_seq=lastSeenSeq+1`

Subscribers see no gap unless the server has already trimmed past the last
seq they observed.

## Node usage

Node 22+ has a native global `WebSocket`, so this library works in Node
too without any polyfill. For older Node, pass a WebSocket constructor
explicitly:

```js
import { WebSocket } from 'ws';
const c = await Client.dial(url, { websocket: WebSocket });
```

## Test

Requires a running event_watch server on `:8080`.

```bash
cd clients/browser
node --test test/client.test.js
# → 7 tests
```

## TypeScript

Types ship alongside as `src/index.d.ts` — no build step, no `.ts`
sources. If you're on TypeScript with `moduleResolution: bundler`, the
types resolve automatically via the `exports` field in `package.json`.
