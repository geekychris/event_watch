import { jsx as _jsx } from "react/jsx-runtime";
// One EventWatch Client instance shared through React context.
// Dial happens explicitly (user clicks Connect); the hook exposes both the
// connection state and the client handle so components can subscribe/publish.
import { createContext, useCallback, useContext, useMemo, useState } from 'react';
import { Client } from '@eventwatch/browser';
const Ctx = createContext(null);
export function ClientProvider({ children }) {
    const [client, setClient] = useState(null);
    const [status, setStatus] = useState('disconnected');
    const [error, setError] = useState('');
    const connect = useCallback(async (url, token) => {
        setClient((c) => { c?.close(); return null; });
        setStatus('connecting');
        setError('');
        try {
            const c = await Client.dial(url, token ? { token } : {});
            setClient(c);
            setStatus('connected');
        }
        catch (e) {
            const msg = e instanceof Error ? e.message : String(e);
            setError(msg);
            setStatus('error');
        }
    }, []);
    const disconnect = useCallback(() => {
        setClient((c) => { c?.close(); return null; });
        setStatus('disconnected');
        setError('');
    }, []);
    const value = useMemo(() => ({ client, status, error, connect, disconnect }), [client, status, error, connect, disconnect]);
    return _jsx(Ctx.Provider, { value: value, children: children });
}
export function useClient() {
    const v = useContext(Ctx);
    if (!v)
        throw new Error('useClient must be inside <ClientProvider>');
    return v;
}
