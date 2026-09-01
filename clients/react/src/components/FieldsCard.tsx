import { useState } from 'react';
import { useClient } from '../hooks/useClient';
import { useSubscribedState } from '../hooks/useSubscribedState';

// A single Fields card wraps set/get/incr/decr for str/int/time topics.
// The current value pane uses useSubscribedState so it updates live as
// values change (from this card, another client, or the CLI).

interface FieldState {
  value?: unknown;
  exists?: boolean;
  updated_at?: string;
}

export function FieldsCard() {
  const { client } = useClient();
  const [topic, setTopic] = useState('int/hits');
  const [value, setValue] = useState('42');
  const [delta, setDelta] = useState('1');
  const state = useSubscribedState<FieldState>(topic);

  const typeOf = topic.split('/')[0] || '';

  const guarded = (fn: () => Promise<unknown>) => async () => {
    if (!client) return alert('connect first');
    try { await fn(); } catch (e) { alert((e as Error).message); }
  };

  const doSet = guarded(async () => {
    if (typeOf === 'str')  return client!.stringField(topic).set(value);
    if (typeOf === 'int')  return client!.intField(topic).set(parseInt(value, 10) || 0);
    if (typeOf === 'time') return client!.timeField(topic).set(value); // RFC3339 string
    throw new Error(`unsupported topic type "${typeOf}"; use str/, int/, or time/`);
  });
  const doIncr = guarded(async () => {
    if (typeOf !== 'int') throw new Error('incr only supported on int/');
    return client!.intField(topic).incr(parseInt(delta, 10) || 1);
  });
  const doDecr = guarded(async () => {
    if (typeOf !== 'int') throw new Error('decr only supported on int/');
    return client!.intField(topic).decr(parseInt(delta, 10) || 1);
  });
  const doNow = guarded(async () => {
    if (typeOf !== 'time') throw new Error('time-now only supported on time/');
    return client!.timeField(topic).now();
  });
  const doDelete = guarded(async () => {
    if (typeOf === 'str')  return client!.stringField(topic).delete();
    if (typeOf === 'int')  return client!.intField(topic).delete();
    if (typeOf === 'time') return client!.timeField(topic).delete();
    throw new Error('unsupported type');
  });

  return (
    <section className="card">
      <h2>4. Fields (str / int / time)</h2>
      <p className="hint">Topic prefix picks the type. Values are folded into a snapshot; every op is also a subscribable event.</p>
      <label>Topic
        <input value={topic} onChange={(e) => setTopic(e.target.value)}
               autoCapitalize="off" autoCorrect="off" spellCheck={false} />
      </label>
      <label>Value (for Set)
        <input value={value} onChange={(e) => setValue(e.target.value)}
               autoCapitalize="off" autoCorrect="off" spellCheck={false} />
      </label>
      <label>Delta (for Incr/Decr)
        <input value={delta} onChange={(e) => setDelta(e.target.value)}
               autoCapitalize="off" autoCorrect="off" spellCheck={false} />
      </label>
      <div className="row">
        <button onClick={doSet}>Set</button>
        <button onClick={doIncr}>Incr</button>
        <button onClick={doDecr}>Decr</button>
        <button onClick={doNow}>Time-now</button>
        <button onClick={doDelete}>Delete</button>
      </div>
      <h3>Current value <span className="hint">(live)</span></h3>
      <pre>{state ? renderValue(typeOf, state) : '(none)'}</pre>
    </section>
  );
}

function renderValue(kind: string, s: FieldState): string {
  const exists = s.exists ? 'set' : 'unset';
  if (kind === 'int')  return `${s.value ?? 0}   (${exists})`;
  if (kind === 'str')  return `${JSON.stringify(s.value)}   (${exists})`;
  if (kind === 'time') return `${s.value ?? '-'}   (${exists})`;
  return JSON.stringify(s, null, 2);
}
