// event_watch browser (and Node 22+) client.
//
// One Client wraps one WebSocket. Subscribe returns a Handle so N callbacks
// on the same topic share ONE upstream subscription — the client only sends
// unsubscribe upstream when the last handle closes. Reconnect is automatic:
// on drop, each topic is re-subscribed with from_seq=lastSeen+1 so callers
// don't see gaps (unless the server has already trimmed past that seq).

import { StringField, IntField, TimeField } from './fields.js';

const DEFAULT_BACKOFF_INITIAL_MS = 100;
const DEFAULT_BACKOFF_MAX_MS = 30_000;
const REQUEST_TIMEOUT_MS = 10_000;

export class Client {
  /**
   * @param {string} url — ws:// or wss:// URL, usually ending in /ws
   * @param {object} [opts]
   * @param {string} [opts.token] — bearer token; sent as ?access_token=… (headers unavailable to browser WS)
   * @param {typeof WebSocket} [opts.websocket] — override the WebSocket constructor (mainly for tests / older Node)
   * @param {number} [opts.backoffInitialMs]
   * @param {number} [opts.backoffMaxMs]
   * @returns {Promise<Client>}
   */
  static async dial(url, opts = {}) {
    const WS = opts.websocket || globalThis.WebSocket;
    if (!WS) {
      throw new Error(
        '@eventwatch/browser: no WebSocket implementation found. ' +
        'In a browser this is native; in Node <22 pass opts.websocket = require("ws").WebSocket.',
      );
    }
    const c = new Client(url, WS, {
      token: opts.token || '',
      backoffInitialMs: opts.backoffInitialMs ?? DEFAULT_BACKOFF_INITIAL_MS,
      backoffMaxMs: opts.backoffMaxMs ?? DEFAULT_BACKOFF_MAX_MS,
    });
    await c._connectOnce();       // eager dial so caller learns immediately on bad URL/auth
    return c;
  }

  constructor(url, WS, opts) {
    this._url = url;
    this._WS = WS;
    this._token = opts.token;
    this._backoffInitial = opts.backoffInitialMs;
    this._backoffMax = opts.backoffMaxMs;

    this._ws = null;
    this._closed = false;
    this._backoff = this._backoffInitial;

    // Subscription state — refcounted per topic.
    // { topic: { fromKind, callbacks: Map<id, cb>, lastSeenSeq } }
    this._topics = new Map();
    this._nextId = 0;

    // In-flight request/response — req_id -> {resolve, reject, timer}
    this._pending = new Map();
    this._nextReq = 0;

    // Frames queued while the socket is (re)connecting.
    this._sendQueue = [];
  }

  // ---- lifecycle ----

  _dialUrl() {
    if (!this._token) return this._url;
    const sep = this._url.includes('?') ? '&' : '?';
    return this._url + sep + 'access_token=' + encodeURIComponent(this._token);
  }

  _connectOnce() {
    return new Promise((resolve, reject) => {
      const ws = new this._WS(this._dialUrl());
      const onOpen = () => {
        this._ws = ws;
        this._backoff = this._backoffInitial;
        ws.removeEventListener('error', onError);
        this._wireSocket(ws);
        // Re-issue every active subscription; then flush anything queued.
        this._resubscribeAll();
        this._flushQueue();
        resolve();
      };
      const onError = (err) => {
        ws.removeEventListener('open', onOpen);
        try { ws.close(); } catch (_) {}
        reject(err instanceof Error ? err : new Error('connect failed'));
      };
      ws.addEventListener('open', onOpen, { once: true });
      ws.addEventListener('error', onError, { once: true });
    });
  }

  _wireSocket(ws) {
    ws.addEventListener('message', (m) => {
      let frame;
      try { frame = JSON.parse(typeof m.data === 'string' ? m.data : ''); } catch { return; }
      this._dispatch(frame);
    });
    const drop = () => {
      // Reject any in-flight requests so callers see the failure fast.
      for (const [, p] of this._pending) {
        clearTimeout(p.timer);
        p.reject(new Error('connection lost'));
      }
      this._pending.clear();
      this._ws = null;
      if (!this._closed) this._reconnectLoop();
    };
    ws.addEventListener('close', drop);
    ws.addEventListener('error', () => { try { ws.close(); } catch (_) {} });
  }

  async _reconnectLoop() {
    while (!this._closed) {
      await new Promise((r) => setTimeout(r, this._backoff));
      this._backoff = Math.min(this._backoff * 2, this._backoffMax);
      try {
        await this._connectOnce();
        return;
      } catch (_) {
        // keep looping; backoff grows to _backoffMax
      }
    }
  }

  close() {
    this._closed = true;
    for (const [, p] of this._pending) {
      clearTimeout(p.timer);
      p.reject(new Error('client closed'));
    }
    this._pending.clear();
    if (this._ws) {
      try { this._ws.close(1000, 'bye'); } catch (_) {}
      this._ws = null;
    }
  }

  // ---- send / dispatch ----

  _send(frame) {
    const text = JSON.stringify(frame);
    if (this._ws && this._ws.readyState === 1 /* OPEN */) {
      try { this._ws.send(text); return; } catch (_) {}
    }
    this._sendQueue.push(text);
  }

  _flushQueue() {
    if (!this._ws || this._ws.readyState !== 1) return;
    while (this._sendQueue.length) {
      try { this._ws.send(this._sendQueue.shift()); } catch (_) { break; }
    }
  }

  _request(frame) {
    return new Promise((resolve, reject) => {
      const reqId = 'r' + (++this._nextReq);
      frame.req_id = reqId;
      const timer = setTimeout(() => {
        this._pending.delete(reqId);
        reject(new Error('request timed out'));
      }, REQUEST_TIMEOUT_MS);
      this._pending.set(reqId, { resolve, reject, timer });
      this._send(frame);
    });
  }

  _dispatch(frame) {
    switch (frame.type) {
      case 'event': {
        const ev = frame.event;
        if (!ev) return;
        const ts = this._topics.get(ev.topic);
        if (!ts) return;
        if (ev.seq > ts.lastSeenSeq) ts.lastSeenSeq = ev.seq;
        for (const cb of ts.callbacks.values()) {
          try { cb(ev); } catch (_) { /* swallow — one bad cb shouldn't kill others */ }
        }
        return;
      }
      case 'state':
      case 'ack':
      case 'error': {
        const reqId = frame.req_id;
        if (!reqId) return; // subscribe acks without req_id — ignore
        const p = this._pending.get(reqId);
        if (!p) return;
        this._pending.delete(reqId);
        clearTimeout(p.timer);
        if (frame.type === 'error') p.reject(new Error(frame.message || 'error'));
        else p.resolve(frame);
        return;
      }
      case 'lagging':
        // No callback delivery; consumer should refetch state if they care.
        return;
    }
  }

  // ---- public API ----

  /**
   * Register cb for events on topic. Returns a Handle; call .close() to remove.
   * N callbacks on one topic share ONE upstream subscription (refcounted).
   *
   * @param {string} topic
   * @param {(event: object) => void} cb
   * @param {object} [opts]
   * @param {string} [opts.from] — "latest" | "last:N" | "seq:N"
   * @returns {Handle}
   */
  subscribe(topic, cb, opts = {}) {
    if (typeof topic !== 'string' || !topic.includes('/')) {
      throw new Error('invalid topic');
    }
    const fromKind = opts.from || 'latest';
    const id = ++this._nextId;
    let ts = this._topics.get(topic);
    const first = !ts;
    if (first) {
      ts = { fromKind, callbacks: new Map(), lastSeenSeq: 0 };
      this._topics.set(topic, ts);
    }
    ts.callbacks.set(id, cb);
    if (first) this._send(_subscribeFrame(topic, fromKind, 0));
    return new Handle(this, topic, id);
  }

  _removeCallback(topic, id) {
    const ts = this._topics.get(topic);
    if (!ts) return;
    ts.callbacks.delete(id);
    if (ts.callbacks.size === 0) {
      this._topics.delete(topic);
      this._send({ op: 'unsubscribe', topic });
    }
  }

  _resubscribeAll() {
    for (const [topic, ts] of this._topics) {
      const resume = ts.lastSeenSeq > 0 ? ts.lastSeenSeq + 1 : 0;
      this._send(_subscribeFrame(topic, ts.fromKind, resume));
    }
  }

  /**
   * Publish one event. Returns the assigned seq.
   */
  async publish(topic, type, payload) {
    const frame = { op: 'publish', topic, type };
    if (payload !== undefined) frame.payload = payload;
    const resp = await this._request(frame);
    return resp.last_seq || 0;
  }

  /**
   * Fetch the current computed state, or null if none yet.
   */
  async getState(topic) {
    try {
      const resp = await this._request({ op: 'get_state', topic });
      return resp.state ?? null;
    } catch (err) {
      if (String(err.message || err).toLowerCase().includes('not found')) return null;
      throw err;
    }
  }

  // ---- typed field helpers ----
  stringField(topic) { return new StringField(this, topic); }
  intField(topic)    { return new IntField(this, topic); }
  timeField(topic)   { return new TimeField(this, topic); }
}

export class Handle {
  constructor(client, topic, id) {
    this._client = client;
    this._topic = topic;
    this._id = id;
    this._closed = false;
  }
  close() {
    if (this._closed) return;
    this._closed = true;
    this._client._removeCallback(this._topic, this._id);
  }
}

function _subscribeFrame(topic, fromKind, resumeSeq) {
  const f = { op: 'subscribe', topic };
  if (resumeSeq > 0) f.from_seq = resumeSeq;
  else if (fromKind.startsWith('last:') || fromKind.startsWith('seq:')) f.from = fromKind;
  else f.from = 'latest';
  return f;
}
