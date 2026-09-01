// NotificationWidget — the "shallow / CDC integration" example.
//
// Pattern: you don't care about the payload or state — you just want to
// know "something happened on this topic so I should invalidate a cache /
// refetch / bounce the UI". Perfect for change-data-capture scenarios
// where the actual data lives elsewhere and event_watch is a notification
// bus only.
//
// The widget flashes each time a notification arrives, shows how many
// have been received, and how long ago the last one was.
import { useEffect, useState } from 'react';
import { useNotification } from '../hooks/useNotification';

export function NotificationWidget() {
  const [topic, setTopic] = useState('chat/notifications');
  const n = useNotification(topic);
  const [flash, setFlash] = useState(false);
  const [now, setNow] = useState(Date.now());

  // Flash on every new notification (500ms).
  useEffect(() => {
    if (n.lastAt === null) return;
    setFlash(true);
    const t = setTimeout(() => setFlash(false), 500);
    return () => clearTimeout(t);
  }, [n.count, n.lastAt]);

  // Tick "N seconds ago" once a second.
  useEffect(() => {
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, []);

  const ago = n.lastAt === null ? '—' : relative(now - n.lastAt);

  return (
    <section className="card">
      <h2>Widget 2 — Notification (change-data-capture)</h2>
      <p className="hint">
        A minimal "just tell me when something happens" pattern. This widget
        ignores the event's payload and state — it only counts occurrences
        and flashes on arrival. Perfect for cache invalidation, "please
        refresh me" triggers, or lit-up-when-active status indicators.
      </p>
      <label>Watch topic
        <input value={topic} onChange={(e) => setTopic(e.target.value)}
               autoCapitalize="off" autoCorrect="off" spellCheck={false}
               placeholder="chat/notifications" />
      </label>
      <div className={`notif-box ${flash ? 'flash' : ''}`}>
        <div className="notif-count">{n.count}</div>
        <div className="notif-label">notification{n.count === 1 ? '' : 's'} received</div>
        <div className="hint" style={{marginTop: 6}}>
          {n.lastEventType ? <>Last: <code>{n.lastEventType}</code> · {ago}</> : 'waiting…'}
        </div>
      </div>
      <p className="hint" style={{marginTop: 10}}>
        <b>To test:</b> use the Publish card (or any other client) to publish
        any event to <code>{topic || '<topic>'}</code>. The panel will pulse
        and the counter tick.
      </p>
    </section>
  );
}

function relative(ms: number): string {
  if (ms < 1000) return 'just now';
  if (ms < 60_000) return `${Math.floor(ms / 1000)}s ago`;
  if (ms < 3_600_000) return `${Math.floor(ms / 60_000)}m ago`;
  return `${Math.floor(ms / 3_600_000)}h ago`;
}
