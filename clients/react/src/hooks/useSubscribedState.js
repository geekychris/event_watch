// useSubscribedState<T>(topic) — subscribes for the lifetime of the component
// and returns the current computed state as T | null.
//
// This is the "deep integration" pattern: your component gets the actual
// reduced state (from the event.state field on every live event), not just
// a notification. Fetches an initial GetState so the first render has data
// even if no live event has fired since connect.
import { useEffect, useState } from 'react';
import { useClient } from './useClient';
export function useSubscribedState(topic) {
    const { client } = useClient();
    const [state, setState] = useState(null);
    useEffect(() => {
        if (!client || !topic) {
            setState(null);
            return;
        }
        let cancelled = false;
        // Seed with the current state (if any) before wiring the subscription.
        client.getState(topic).then((s) => {
            if (!cancelled)
                setState(s);
        }).catch(() => { });
        const handle = client.subscribe(topic, (ev) => {
            if (ev.state !== undefined)
                setState(ev.state);
        });
        return () => {
            cancelled = true;
            handle.close();
        };
    }, [client, topic]);
    return state;
}
