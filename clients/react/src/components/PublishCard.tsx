// Publish card + scripted simulations. Reads/writes the shared publish form
// context so the CheatsheetCard can programmatically inject examples.
import { useClient } from '../hooks/useClient';
import { usePublishForm } from '../hooks/usePublishForm';

const SIMS: Record<string, { topic: string; steps: [string, Record<string, unknown>][] }> = {
  pr:     { topic: 'pr/octo/hello/1', steps: [
    ['pr_opened', { title: 'Add feature', author: 'alice', base: 'main', head: 'abc123' }],
    ['pr_review_requested', { reviewer: 'bob' }],
    ['check_run_completed', { conclusion: 'success', name: 'test' }],
    ['pr_commented', {}],
    ['pr_reviewed', { state: 'approved' }],
    ['pr_merged', {}],
  ]},
  build:  { topic: 'build/ci/42', steps: [
    ['build_queued', {}], ['build_started', {}],
    ['step_started', {step: 'compile'}], ['step_finished', {step: 'compile', status: 'success'}],
    ['step_started', {step: 'test'}], ['step_finished', {step: 'test', status: 'success'}],
    ['build_finished', {status: 'success'}],
  ]},
  deploy: { topic: 'deploy/prod/api', steps: [
    ['deploy_started', {version: 'v42', env: 'prod', service: 'api'}],
    ['health_check_pass', {}], ['deploy_finished', {status: 'success'}],
  ]},
  job:    { topic: 'job/reindex-1', steps: [
    ['job_started', {name: 'reindex'}],
    ['job_progress', {percent: 33}], ['job_log', {line: 'processing shard 1'}],
    ['job_progress', {percent: 66}], ['job_log', {line: 'processing shard 2'}],
    ['job_progress', {percent: 100}], ['job_finished', {}],
  ]},
  chat:   { topic: 'chat/general', steps: [
    ['user_joined', {user: 'alice'}], ['user_joined', {user: 'bob'}],
    ['msg_posted', {id: 'm1', user: 'alice', text: 'hey team'}],
    ['msg_posted', {id: 'm2', user: 'bob', text: 'hi'}],
    ['msg_edited', {id: 'm1', text: 'hey team!'}],
  ]},
};

export function PublishCard() {
  const { client } = useClient();
  const form = usePublishForm();

  const publish = async () => {
    if (!client) return alert('connect first');
    let p: unknown;
    try { p = JSON.parse(form.payload || '{}'); } catch (e) { return alert('payload must be JSON: ' + (e as Error).message); }
    try { await client.publish(form.topic, form.type, p); } catch (e) { alert('publish: ' + (e as Error).message); }
  };

  const runSim = async (kind: keyof typeof SIMS) => {
    if (!client) return alert('connect first');
    const sim = SIMS[kind];
    for (const [t, p] of sim.steps) {
      try { await client.publish(sim.topic, t, p); }
      catch (e) { return alert('sim: ' + (e as Error).message); }
      await new Promise((r) => setTimeout(r, 250));
    }
  };

  return (
    <section className="card" id="publish-card">
      <h2>3. Publish / simulate</h2>
      <label>
        Topic <Help>Format: <code>&lt;type&gt;/&lt;id...&gt;</code>. Case-sensitive; lowercase prefixes only (pr/int/str/...). Segments: [A-Za-z0-9._-]+, max 512 chars.</Help>
        <input value={form.topic} onChange={(e) => form.set({ topic: e.target.value })}
               autoCapitalize="off" autoCorrect="off" spellCheck={false} />
      </label>
      <label>
        Event type <Help>Reducer-recognised event name for the topic's type. e.g. <code>pr_opened</code>, <code>int_incr</code>. See the Cheatsheet card below for the full list.</Help>
        <input value={form.type} onChange={(e) => form.set({ type: e.target.value })}
               autoCapitalize="off" autoCorrect="off" spellCheck={false} />
      </label>
      <label>
        Payload JSON <Help>Object literal. Key names are exact — see the Cheatsheet for what each event type reads (e.g. <code>&#123;"delta":5&#125;</code> for int_incr, not <code>&#123;"value":5&#125;</code>).</Help>
        <textarea rows={3} value={form.payload} onChange={(e) => form.set({ payload: e.target.value })}
                  autoCapitalize="off" autoCorrect="off" spellCheck={false} />
      </label>
      <div className="row"><button onClick={publish}>Publish</button></div>

      <h3>Scripted simulations</h3>
      <p className="hint">Each button drives a full lifecycle on a default topic.</p>
      <div className="row">
        {(Object.keys(SIMS) as (keyof typeof SIMS)[]).map((k) => (
          <button key={k} onClick={() => runSim(k)}>{k.toUpperCase()}</button>
        ))}
      </div>
    </section>
  );
}

// Small inline "?" tooltip. Uses <details>/<summary> so it works without
// any tooltip library and stays open on click for readability.
function Help({ children }: { children: React.ReactNode }) {
  return (
    <details className="help">
      <summary title="click for help">ⓘ</summary>
      <div className="help-body">{children}</div>
    </details>
  );
}
