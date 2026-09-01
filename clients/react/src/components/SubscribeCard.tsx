// Raw event-feed subscriber. Same UX as the Wails app: type a topic, click
// Subscribe, see events stream in.
import { useEffect, useRef, useState } from 'react';
import type { Event as EWEvent } from '@eventwatch/browser';
import { useClient } from '../hooks/useClient';

export function SubscribeCard() {
  const { client } = useClient();
  const [topic, setTopic] = useState('pr/octo/hello/1');
  const [from, setFrom] = useState('latest');
  const [events, setEvents] = useState<EWEvent[]>([]);
  const handleRef = useRef<{ close(): void } | null>(null);
  const feedRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (feedRef.current) feedRef.current.scrollTop = feedRef.current.scrollHeight;
  }, [events]);

  const subscribe = () => {
    if (!client) return alert('connect first');
    if (handleRef.current) handleRef.current.close();
    setEvents([]);
    handleRef.current = client.subscribe(
      topic,
      (ev) => setEvents((prev) => [...prev, ev].slice(-500)),
      { from },
    );
  };
  const unsubscribe = () => {
    if (handleRef.current) { handleRef.current.close(); handleRef.current = null; }
  };
  useEffect(() => () => handleRef.current?.close(), []);

  const badge = (t: string) => {
    const kind = t.indexOf('/') > 0 ? t.slice(0, t.indexOf('/')) : '';
    return <span className={`badge ${kind}`}>{ /* placeholder — filled below */ null}</span>;
  };

  return (
    <section className="card">
      <h2>2. Subscribe</h2>
      <label>Topic
        <input value={topic} onChange={(e) => setTopic(e.target.value)}
               autoCapitalize="off" autoCorrect="off" spellCheck={false} />
      </label>
      <label>From
        <select value={from} onChange={(e) => setFrom(e.target.value)}>
          <option value="latest">latest (new only)</option>
          <option value="last:10">last 10</option>
          <option value="last:50">last 50</option>
          <option value="seq:1">from seq 1</option>
        </select>
      </label>
      <div className="row">
        <button onClick={subscribe}>Subscribe</button>
        <button onClick={unsubscribe}>Unsubscribe</button>
      </div>
      <h3>Live events</h3>
      <div className="feed" ref={feedRef}>
        {events.map((e, i) => (
          <div className="ev" key={i}>
            <span className={`badge ${e.topic.split('/')[0] || ''}`}>{e.type}</span>
            <code>{e.topic}</code> seq={e.seq}
            {e.payload !== undefined && <code> {JSON.stringify(e.payload)}</code>}
            {e.state !== undefined && (
              <> → <b>{summarise(e.topic, e.state)}</b></>
            )}
          </div>
        ))}
      </div>
      {badge('') && null}
    </section>
  );
}

function summarise(topic: string, state: unknown): string {
  const kind = topic.split('/')[0] || '';
  if (typeof state !== 'object' || state === null) return String(state);
  const s = state as Record<string, unknown>;
  if (kind === 'int' && typeof s.value === 'number') return String(s.value);
  if (kind === 'str' && 'value' in s) return JSON.stringify(s.value);
  if (kind === 'time' && s.value) return String(s.value);
  return JSON.stringify(s);
}
