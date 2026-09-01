import './style.css';
import './app.css';

import {
  Connect, Disconnect, Subscribe, Unsubscribe,
  Publish, GetState, ListTopics, Metrics,
  SetString, SetInt, IncrInt, DecrInt, TimeNow, DeleteField,
} from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

// -- template --

document.querySelector('#app').innerHTML = `
<header>
  <h1>event_watch</h1>
  <div id="conn-status" class="pill offline">disconnected</div>
</header>
<main class="grid">

  <section class="card">
    <h2>1. Connect</h2>
    <label>Server URL <input id="ws-url" value="ws://localhost:8080/ws" autocapitalize="off" autocorrect="off" spellcheck="false" /></label>
    <label>Auth token (optional) <input id="ws-token" placeholder="leave blank when --auth is off" autocapitalize="off" autocorrect="off" spellcheck="false" /></label>
    <div class="row">
      <button id="btn-connect">Connect</button>
      <button id="btn-disconnect" disabled>Disconnect</button>
    </div>
  </section>

  <section class="card">
    <h2>2. Subscribe</h2>
    <label>Topic <input id="sub-topic" value="pr/octo/hello/1" placeholder="pr/octo/hello/1" autocapitalize="off" autocorrect="off" spellcheck="false" /></label>
    <label>From
      <select id="sub-from">
        <option value="latest">latest (new only)</option>
        <option value="last:10">last 10</option>
        <option value="last:50">last 50</option>
        <option value="seq:1">from seq 1</option>
      </select>
    </label>
    <div class="row">
      <button id="btn-subscribe">Subscribe</button>
      <button id="btn-unsubscribe">Unsubscribe</button>
    </div>
    <h3>Live events</h3>
    <div id="event-feed" class="feed"></div>
  </section>

  <section class="card">
    <h2>3. Publish / simulate</h2>
    <label>Topic <input id="pub-topic" value="chat/general" placeholder="chat/general" autocapitalize="off" autocorrect="off" spellcheck="false" /></label>
    <label>Event type <input id="pub-type" value="msg_posted" placeholder="msg_posted" autocapitalize="off" autocorrect="off" spellcheck="false" /></label>
    <label>Payload JSON <textarea id="pub-payload" rows="3" autocapitalize="off" autocorrect="off" spellcheck="false">{"user":"alice","text":"hi"}</textarea></label>
    <div class="row"><button id="btn-publish">Publish</button></div>

    <h3>Scripted simulations</h3>
    <p class="hint">Each button drives a full lifecycle on a default topic.</p>
    <div class="row">
      <button data-sim="pr">PR</button>
      <button data-sim="build">Build</button>
      <button data-sim="deploy">Deploy</button>
      <button data-sim="job">Job</button>
      <button data-sim="chat">Chat</button>
    </div>
  </section>

  <section class="card">
    <h2>4. Fields (str / int / time)</h2>
    <p class="hint">Topic prefix picks the type. Values are folded into a snapshot; every op is also a subscribable event.</p>
    <label>Topic <input id="field-topic" value="int/hits" placeholder="int/hits" autocapitalize="off" autocorrect="off" spellcheck="false" /></label>
    <label>Value (for Set) <input id="field-value" value="42" autocapitalize="off" autocorrect="off" spellcheck="false" /></label>
    <label>Delta (for Incr/Decr) <input id="field-delta" value="1" autocapitalize="off" autocorrect="off" spellcheck="false" /></label>
    <div class="row">
      <button id="btn-field-get">Get</button>
      <button id="btn-field-set">Set</button>
      <button id="btn-field-incr">Incr</button>
      <button id="btn-field-decr">Decr</button>
      <button id="btn-field-now">Time-now</button>
      <button id="btn-field-delete">Delete</button>
    </div>
    <h3>Current value <span id="field-live" class="hint">(subscribe to see live)</span></h3>
    <pre id="field-out">(none)</pre>
  </section>

  <section class="card">
    <h2>5. State + metrics</h2>
    <label>Topic <input id="state-topic" value="pr/octo/hello/1" placeholder="pr/octo/hello/1" autocapitalize="off" autocorrect="off" spellcheck="false" /></label>
    <div class="row"><button id="btn-get-state">Get state</button></div>
    <pre id="state-out">(none)</pre>

    <h3>Metrics <span class="hint">(refresh 2s)</span></h3>
    <table id="metrics-table"><tbody></tbody></table>

    <h3>Topics</h3>
    <ul id="topics-list"></ul>
  </section>

</main>
`;

// -- helpers --

const $ = (id) => document.getElementById(id);
const feed = $('event-feed');
const status = $('conn-status');
const btnConnect = $('btn-connect');
const btnDisconnect = $('btn-disconnect');

let connected = false;
const subscribedTopics = new Set();
const eventUnsubs = new Map(); // topic → wails runtime unsubscribe fn

function setStatus(kind, text) {
  status.className = 'pill ' + kind;
  status.textContent = text;
}

function objectTypeOf(topic) {
  const i = topic.indexOf('/');
  return i > 0 ? topic.slice(0, i) : '';
}

function escapeHtml(s) {
  return s.replace(/[&<>"']/g, (c) => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"})[c]);
}

function appendEvent(e) {
  const div = document.createElement('div');
  div.className = 'ev';
  const badge = `<span class="badge ${objectTypeOf(e.topic)}">${e.type}</span>`;
  const payloadStr = e.payload ? escapeHtml(JSON.stringify(e.payload)) : '';
  // Live event frames now carry the post-reduce state under e.state — show it
  // inline as "→ current" for a compact "op → value" line. Non-live reads
  // (e.g. historical) won't have e.state and only show the raw op.
  let derived = '';
  if (e.state) {
    const t = objectTypeOf(e.topic);
    if (t === 'int' && typeof e.state.value === 'number')  derived = ` → <b>${e.state.value}</b>`;
    else if (t === 'str' && 'value' in e.state)            derived = ` → <b>${escapeHtml(JSON.stringify(e.state.value))}</b>`;
    else if (t === 'time' && e.state.value)                derived = ` → <b>${escapeHtml(e.state.value)}</b>`;
    // For non-field object types (pr/build/deploy/job/chat) the derived
    // snapshot is a whole struct — leave that to the State card.
  }
  div.innerHTML = `${badge}<code>${e.topic}</code> seq=${e.seq} <code>${payloadStr}</code>${derived}`;
  feed.appendChild(div);
  feed.scrollTop = feed.scrollHeight;
}

// -- connect --

btnConnect.onclick = async () => {
  const url = $('ws-url').value.trim();
  const token = $('ws-token').value.trim();
  setStatus('offline', 'connecting…');
  try {
    await Connect(url, token);
    connected = true;
    setStatus('online', 'connected');
    btnConnect.disabled = true;
    btnDisconnect.disabled = false;
    refreshTopics();
  } catch (err) {
    setStatus('offline', 'error');
    alert('connect: ' + err);
  }
};

btnDisconnect.onclick = async () => {
  await Disconnect();
  connected = false;
  subscribedTopics.clear();
  for (const [, unsub] of eventUnsubs) unsub();
  eventUnsubs.clear();
  setStatus('offline', 'disconnected');
  btnConnect.disabled = false;
  btnDisconnect.disabled = true;
};

// -- subscribe --

$('btn-subscribe').onclick = async () => {
  const topic = $('sub-topic').value.trim();
  const from = $('sub-from').value;
  if (!topic) { alert('type a topic first (e.g. pr/octo/hello/1)'); return; }
  if (!connected) { alert('connect first'); return; }
  if (subscribedTopics.has(topic)) { alert('already subscribed to ' + topic); return; }
  try {
    // Wire the event listener BEFORE Subscribe so the first backfill frame
    // doesn't race past us.
    const unsub = EventsOn('event:' + topic, (e) => appendEvent(e));
    eventUnsubs.set(topic, unsub);
    await Subscribe(topic, from);
    subscribedTopics.add(topic);
  } catch (err) {
    // Rollback listener on failure.
    const u = eventUnsubs.get(topic); if (u) { u(); eventUnsubs.delete(topic); }
    alert('subscribe: ' + err);
  }
};

$('btn-unsubscribe').onclick = async () => {
  const topic = $('sub-topic').value.trim();
  if (!topic) return;
  await Unsubscribe(topic);
  subscribedTopics.delete(topic);
  const u = eventUnsubs.get(topic); if (u) { u(); eventUnsubs.delete(topic); }
};

// -- publish --

$('btn-publish').onclick = async () => {
  const topic = $('pub-topic').value.trim();
  const type = $('pub-type').value.trim();
  const raw = $('pub-payload').value || '{}';
  try { JSON.parse(raw); } catch (e) { return alert('payload must be valid JSON: ' + e.message); }
  if (!topic || !type) return alert('topic and type required');
  try { await Publish(topic, type, raw); } catch (e) { alert('publish: ' + e); }
};

// -- simulations --

const SIMS = {
  pr: { topic: 'pr/octo/hello/1', steps: [
    ['pr_opened', { title: 'Add feature', author: 'alice', base: 'main', head: 'abc123' }],
    ['pr_review_requested', { reviewer: 'bob' }],
    ['check_run_completed', { conclusion: 'success', name: 'test' }],
    ['pr_commented', {}],
    ['pr_reviewed', { state: 'approved' }],
    ['pr_merged', {}],
  ]},
  build: { topic: 'build/ci/42', steps: [
    ['build_queued', {}],
    ['build_started', {}],
    ['step_started', { step: 'compile' }],
    ['step_finished', { step: 'compile', status: 'success' }],
    ['step_started', { step: 'test' }],
    ['step_finished', { step: 'test', status: 'success' }],
    ['build_finished', { status: 'success' }],
  ]},
  deploy: { topic: 'deploy/prod/api', steps: [
    ['deploy_started', { version: 'v42', env: 'prod', service: 'api' }],
    ['health_check_pass', {}],
    ['deploy_finished', { status: 'success' }],
  ]},
  job: { topic: 'job/reindex-1', steps: [
    ['job_started', { name: 'reindex' }],
    ['job_progress', { percent: 33 }],
    ['job_log', { line: 'processing shard 1' }],
    ['job_progress', { percent: 66 }],
    ['job_log', { line: 'processing shard 2' }],
    ['job_progress', { percent: 100 }],
    ['job_finished', {}],
  ]},
  chat: { topic: 'chat/general', steps: [
    ['user_joined', { user: 'alice' }],
    ['user_joined', { user: 'bob' }],
    ['msg_posted', { id: 'm1', user: 'alice', text: 'hey team' }],
    ['msg_posted', { id: 'm2', user: 'bob', text: 'hi' }],
    ['msg_edited', { id: 'm1', text: 'hey team!' }],
  ]},
};

document.querySelectorAll('[data-sim]').forEach((btn) => {
  btn.onclick = async () => {
    const kind = btn.dataset.sim;
    const sim = SIMS[kind];
    if (!sim) return;
    for (const [type, payload] of sim.steps) {
      try { await Publish(sim.topic, type, JSON.stringify(payload)); }
      catch (e) { alert('sim: ' + e); return; }
      await new Promise((r) => setTimeout(r, 250));
    }
  };
});

// -- fields --

function fieldType(topic) {
  const i = topic.indexOf('/');
  return i > 0 ? topic.slice(0, i) : '';
}

function renderFieldState(topic, parsed) {
  const t = fieldType(topic);
  let display;
  if (t === 'int')       display = `${parsed.value}   (${parsed.exists ? 'set' : 'unset'})`;
  else if (t === 'str')  display = `${JSON.stringify(parsed.value)}   (${parsed.exists ? 'set' : 'unset'})`;
  else if (t === 'time') display = `${parsed.value}   (${parsed.exists ? 'set' : 'unset'})`;
  else                   display = JSON.stringify(parsed, null, 2);
  $('field-out').textContent = display;
}

async function renderField(topic) {
  try {
    const s = await GetState(topic);
    if (!s) { $('field-out').textContent = '(no value yet)'; return; }
    renderFieldState(topic, JSON.parse(s));
  } catch (e) { $('field-out').textContent = 'error: ' + e; }
}

// A dedicated field-live subscription. Whenever the topic changes we swap
// the underlying subscription; each event refetches the reduced state. If
// panel 2 has already subscribed to the same topic, our Subscribe call will
// return "already subscribed" — that's fine, EventsOn still fires for us.
let fieldSubTopic = null;
let fieldSubUnsub = null;
async function ensureFieldLiveSub() {
  const topic = $('field-topic').value.trim();
  if (topic === fieldSubTopic) return;
  if (fieldSubUnsub) { fieldSubUnsub(); fieldSubUnsub = null; }
  if (fieldSubTopic) {
    try { await Unsubscribe(fieldSubTopic); } catch (_) {}
    fieldSubTopic = null;
  }
  if (!topic || !connected) { $('field-live').textContent = ''; return; }
  fieldSubUnsub = EventsOn('event:' + topic, (e) => {
    // Events now carry post-reduce state — use it directly to avoid the
    // extra GetState round-trip. Fall back to GetState if a caller lands
    // here without one (defensive; broker always sets it currently).
    if (e && e.state) renderFieldState(topic, e.state);
    else renderField(topic);
  });
  try { await Subscribe(topic, 'latest'); } catch (_) { /* duplicate = ok */ }
  fieldSubTopic = topic;
  $('field-live').textContent = '(live)';
}

$('btn-field-get').onclick = () => { const t = $('field-topic').value.trim(); if (t) renderField(t); };
$('btn-field-set').onclick = async () => {
  const t = $('field-topic').value.trim();
  const v = $('field-value').value;
  if (!t || !connected) return alert('need topic + connection');
  try {
    const ty = fieldType(t);
    if (ty === 'str') await SetString(t, v);
    else if (ty === 'int') await SetInt(t, parseInt(v, 10) || 0);
    else if (ty === 'time') await SetString(t, v); // time_set via SetString reuses value string as RFC3339
    else return alert('unsupported field type: ' + ty);
    await ensureFieldLiveSub();
    renderField(t);
  } catch (e) { alert('set: ' + e); }
};
$('btn-field-incr').onclick = async () => {
  const t = $('field-topic').value.trim();
  const d = parseInt($('field-delta').value, 10) || 1;
  if (!t || !connected) return;
  try { await IncrInt(t, d); await ensureFieldLiveSub(); renderField(t); }
  catch (e) { alert('incr: ' + e); }
};
$('btn-field-decr').onclick = async () => {
  const t = $('field-topic').value.trim();
  const d = parseInt($('field-delta').value, 10) || 1;
  if (!t || !connected) return;
  try { await DecrInt(t, d); await ensureFieldLiveSub(); renderField(t); }
  catch (e) { alert('decr: ' + e); }
};
$('btn-field-now').onclick = async () => {
  const t = $('field-topic').value.trim();
  if (!t || !connected) return;
  try { await TimeNow(t); await ensureFieldLiveSub(); renderField(t); }
  catch (e) { alert('time-now: ' + e); }
};
$('btn-field-delete').onclick = async () => {
  const t = $('field-topic').value.trim();
  if (!t || !connected) return;
  try { await DeleteField(t); renderField(t); }
  catch (e) { alert('delete: ' + e); }
};
$('field-topic').addEventListener('change', ensureFieldLiveSub);

// -- state --

$('btn-get-state').onclick = async () => {
  const topic = $('state-topic').value.trim();
  if (!topic) return;
  try {
    const s = await GetState(topic);
    $('state-out').textContent = s ? JSON.stringify(JSON.parse(s), null, 2) : '(no state yet)';
  } catch (e) { $('state-out').textContent = 'error: ' + e; }
};

// -- metrics + topics polling --

async function refreshMetrics() {
  if (!connected) return;
  try {
    const raw = await Metrics();
    const m = JSON.parse(raw);
    const rows = [['connected clients', m.connected_clients], ['topics', m.topics]];
    for (const [k, v] of Object.entries(m.subscriptions_by_type || {})) rows.push(['subs · ' + k, v]);
    for (const [k, v] of Object.entries(m.ingested_by_type || {}))     rows.push(['ingested · ' + k, v]);
    for (const [k, v] of Object.entries(m.fanned_out_by_type || {}))   rows.push(['fanned · ' + k, v]);
    for (const [k, v] of Object.entries(m.dropped_by_type || {}))      rows.push(['dropped · ' + k, v]);
    $('metrics-table').innerHTML = '<tbody>' +
      rows.map(([k, v]) => `<tr><td>${k}</td><td>${v}</td></tr>`).join('') + '</tbody>';
  } catch (_) {}
}

async function refreshTopics() {
  if (!connected) return;
  try {
    const topics = await ListTopics();
    $('topics-list').innerHTML = (topics || []).map((t) => `<li><code>${t}</code></li>`).join('');
  } catch (_) {}
}

setInterval(refreshMetrics, 2000);
setInterval(refreshTopics, 5000);
