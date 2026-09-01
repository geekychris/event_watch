// Integration tests. Require a running event_watch server on :8080
// (override with EW_URL env var). Node 22+ has a native WebSocket, so
// no polyfill dep is needed.
//
// Run with:  npm test  (or: node --test test/)

import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Client } from '../src/index.js';

const URL = process.env.EW_URL || 'ws://localhost:8080/ws';

async function dialOrSkip(t) {
  try {
    return await Client.dial(URL);
  } catch (err) {
    t.skip(`event_watch server not reachable at ${URL} — skipping`);
    return null;
  }
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

test('int field: set + incr + decr + get is arithmetic', async (t) => {
  const c = await dialOrSkip(t); if (!c) return;
  try {
    const f = c.intField('int/jstest/counter');
    await f.set(100);
    await f.incr(5);
    await f.incr();            // default +1
    await f.decr(4);
    const [v, ok] = await f.get();
    assert.equal(v, 102);
    assert.equal(ok, true);
  } finally { c.close(); }
});

test('string field: set + delete + get', async (t) => {
  const c = await dialOrSkip(t); if (!c) return;
  try {
    const f = c.stringField('str/jstest/name');
    await f.set('alice');
    let [v, ok] = await f.get();
    assert.equal(v, 'alice'); assert.equal(ok, true);
    await f.delete();
    [v, ok] = await f.get();
    assert.equal(v, ''); assert.equal(ok, false);
  } finally { c.close(); }
});

test('time field: set with Date + add + get', async (t) => {
  const c = await dialOrSkip(t); if (!c) return;
  try {
    const f = c.timeField('time/jstest/last');
    const when = new Date('2026-01-01T12:00:00Z');
    await f.set(when);
    await f.add(3600);
    const [d, ok] = await f.get();
    assert.equal(ok, true);
    assert.ok(d instanceof Date);
    assert.equal(d.getTime(), when.getTime() + 3600 * 1000);
  } finally { c.close(); }
});

test('subscribe: live events carry post-reduce state', async (t) => {
  const c = await dialOrSkip(t); if (!c) return;
  try {
    const events = [];
    const h = c.subscribe('int/jstest/live', (ev) => events.push(ev));
    await sleep(80);
    const f = c.intField('int/jstest/live');
    await f.set(10);
    await f.incr(5);
    await f.decr(2);

    const deadline = Date.now() + 3000;
    while (events.length < 3 && Date.now() < deadline) await sleep(20);
    assert.equal(events.length, 3, `expected 3 events, got ${events.length}`);
    const last = events[events.length - 1];
    assert.ok(last.state, 'live event should carry state');
    assert.equal(last.state.value, 13, 'final reduced value should be 13');
    h.close();
  } finally { c.close(); }
});

test('subscribe is refcounted: 2 callbacks -> 1 upstream sub -> both fire', async (t) => {
  const c = await dialOrSkip(t); if (!c) return;
  try {
    let a = 0, b = 0;
    const h1 = c.subscribe('int/jstest/refcount', () => a++);
    const h2 = c.subscribe('int/jstest/refcount', () => b++);
    await sleep(80);
    await c.intField('int/jstest/refcount').set(1);
    const deadline = Date.now() + 2000;
    while ((a === 0 || b === 0) && Date.now() < deadline) await sleep(20);
    assert.equal(a, 1);
    assert.equal(b, 1);

    // Close one — the other still receives events.
    h1.close();
    await sleep(50);
    await c.intField('int/jstest/refcount').set(2);
    const deadline2 = Date.now() + 2000;
    while (b < 2 && Date.now() < deadline2) await sleep(20);
    assert.equal(a, 1, 'closed handle should stop receiving');
    assert.equal(b, 2, 'other handle should still receive');
    h2.close();
  } finally { c.close(); }
});

test('publish error propagates', async (t) => {
  const c = await dialOrSkip(t); if (!c) return;
  try {
    await assert.rejects(
      () => c.publish('no-slash', 'x'),
      (err) => /invalid/i.test(err.message),
    );
  } finally { c.close(); }
});

test('invalid topic on subscribe throws synchronously', async (t) => {
  const c = await dialOrSkip(t); if (!c) return;
  try {
    assert.throws(() => c.subscribe('nope', () => {}), /invalid topic/i);
  } finally { c.close(); }
});
