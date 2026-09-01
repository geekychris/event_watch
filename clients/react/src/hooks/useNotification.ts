// useNotification(topic) — subscribes but doesn't care about payload or state.
// Returns a bumping counter and the timestamp of the last event.
//
// This is the "shallow / CDC integration" pattern: your component just wants
// to know that "something happened on this topic" so it can invalidate a
// cache, refetch data, or bounce a UI element. Same underlying subscription
// as useSubscribedState, but only tracks _that_ an event occurred.
import { useEffect, useState } from 'react';
import { useClient } from './useClient';

export interface Notification {
  count: number;
  lastAt: number | null;    // Date.now() at last event; null before first
  lastEventType: string;    // "" before first
}

export function useNotification(topic: string | null): Notification {
  const { client } = useClient();
  const [n, setN] = useState<Notification>({ count: 0, lastAt: null, lastEventType: '' });

  useEffect(() => {
    if (!client || !topic) return;
    const handle = client.subscribe(topic, (ev) => {
      setN((prev) => ({ count: prev.count + 1, lastAt: Date.now(), lastEventType: ev.type }));
    });
    return () => handle.close();
  }, [client, topic]);

  return n;
}
