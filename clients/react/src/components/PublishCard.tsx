// Publish card + scripted simulations (parity with the Wails app).
import { useState } from 'react';
import { useClient } from '../hooks/useClient';

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
  const [topic, setTopic] = useState('chat/general');
  const [type, setType] = useState('msg_posted');
  const [payload, setPayload] = useState('{"user":"alice","text":"hi"}');

  const publish = async () => {
    if (!client) return alert('connect first');
    let p: unknown;
    try { p = JSON.parse(payload || '{}'); } catch (e) { return alert('payload must be JSON: ' + (e as Error).message); }
    try { await client.publish(topic, type, p); } catch (e) { alert('publish: ' + (e as Error).message); }
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
    <section className="card">
      <h2>3. Publish / simulate</h2>
      <label>Topic
        <input value={topic} onChange={(e) => setTopic(e.target.value)}
               autoCapitalize="off" autoCorrect="off" spellCheck={false} />
      </label>
      <label>Event type
        <input value={type} onChange={(e) => setType(e.target.value)}
               autoCapitalize="off" autoCorrect="off" spellCheck={false} />
      </label>
      <label>Payload JSON
        <textarea rows={3} value={payload} onChange={(e) => setPayload(e.target.value)}
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
