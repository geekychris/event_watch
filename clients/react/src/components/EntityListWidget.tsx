// EntityListWidget — the "deep integration" example.
//
// Pattern: your app holds a list of entities the user cares about (PRs,
// deploys, jobs, ...). Each row corresponds to one topic. Adding a row
// starts a subscription for that topic; the row renders the reduced state
// (title, status, counters, ...) and updates in place as events arrive.
// Removing a row unsubscribes.
//
// This is what you build a "dashboard of things" out of — the client
// library gives you both the current value (via GetState) AND live updates
// (via the state field on subscribed events) in one hook.
import { useState } from 'react';
import { useClient } from '../hooks/useClient';
import { useSubscribedState } from '../hooks/useSubscribedState';

interface PRState {
  title?: string;
  author?: string;
  state?: string;               // open / closed / merged
  approvals?: number;
  reviewers?: string[];
  checks?: { passed?: number; failed?: number; pending?: number };
  comments?: number;
  updated_at?: string;
}

export function EntityListWidget() {
  const { client } = useClient();
  const [nextTopic, setNextTopic] = useState('pr/octo/hello/1');
  const [items, setItems] = useState<string[]>([]);

  const add = () => {
    const t = nextTopic.trim();
    if (!t) return;
    if (items.includes(t)) return alert('already in the list');
    setItems((xs) => [...xs, t]);
  };
  const remove = (t: string) => setItems((xs) => xs.filter((x) => x !== t));

  return (
    <section className="card">
      <h2>Widget 1 — Entity list (deep integration)</h2>
      <p className="hint">
        Add a topic; that row subscribes and shows the current reduced state,
        auto-updating on every event. This is how you build a "dashboard of
        things" — one subscription per row, one server round-trip for the
        initial value, then live updates via <code>event.state</code>.
      </p>
      <label>Topic
        <input value={nextTopic} onChange={(e) => setNextTopic(e.target.value)}
               autoCapitalize="off" autoCorrect="off" spellCheck={false}
               placeholder="pr/octo/hello/1" />
      </label>
      <div className="row">
        <button onClick={add} disabled={!client}>Add to list</button>
        <span className="hint">
          {client ? `${items.length} tracked` : '(connect first)'}
        </span>
      </div>
      <table className="entity-table">
        <thead>
          <tr>
            <th>Topic</th>
            <th>Title / author</th>
            <th>State</th>
            <th>Signals</th>
            <th>Updated</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {items.map((t) => <EntityRow key={t} topic={t} onRemove={() => remove(t)} />)}
          {items.length === 0 && (
            <tr><td colSpan={6} className="hint">
              (no entities — add a topic above; then use another client to
              publish events for that topic and watch the row update)
            </td></tr>
          )}
        </tbody>
      </table>
    </section>
  );
}

function EntityRow({ topic, onRemove }: { topic: string; onRemove: () => void }) {
  // The magic line: subscribes for the row's lifetime, returns latest state.
  const state = useSubscribedState<PRState>(topic);
  const s = state || {};
  return (
    <tr>
      <td><code>{topic}</code></td>
      <td>
        {s.title ? <><b>{s.title}</b><br /><span className="hint">by {s.author || '—'}</span></> : <span className="hint">(no title yet)</span>}
      </td>
      <td><span className={`badge state-${s.state || 'unknown'}`}>{s.state || '—'}</span></td>
      <td className="hint">
        ✓ {s.approvals ?? 0}
        {s.reviewers?.length ? <> · 👀 {s.reviewers.length}</> : null}
        {s.comments ? <> · 💬 {s.comments}</> : null}
        {s.checks && ((s.checks.passed ?? 0) + (s.checks.failed ?? 0) + (s.checks.pending ?? 0) > 0) && (
          <> · <span style={{color: '#7fd'}}>{s.checks.passed ?? 0} ok</span>
             {' / '}<span style={{color: '#f6a'}}>{s.checks.failed ?? 0} fail</span>
             {' / '}{s.checks.pending ?? 0} pending</>
        )}
      </td>
      <td className="hint">{s.updated_at ? relative(s.updated_at) : '—'}</td>
      <td><button onClick={onRemove}>×</button></td>
    </tr>
  );
}

function relative(iso: string): string {
  const ms = Date.now() - new Date(iso).getTime();
  if (Number.isNaN(ms)) return iso;
  if (ms < 1000) return 'just now';
  if (ms < 60_000) return `${Math.floor(ms / 1000)}s ago`;
  if (ms < 3_600_000) return `${Math.floor(ms / 60_000)}m ago`;
  return `${Math.floor(ms / 3_600_000)}h ago`;
}
