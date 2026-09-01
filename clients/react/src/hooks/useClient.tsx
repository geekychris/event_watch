// One EventWatch Client instance shared through React context.
// Dial happens explicitly (user clicks Connect); the hook exposes both the
// connection state and the client handle so components can subscribe/publish.
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react';
import { Client } from '@eventwatch/browser';

type Status = 'disconnected' | 'connecting' | 'connected' | 'error';

interface ClientCtx {
  client: Client | null;
  status: Status;
  error: string;
  connect(url: string, token?: string): Promise<void>;
  disconnect(): void;
}

const Ctx = createContext<ClientCtx | null>(null);

export function ClientProvider({ children }: { children: ReactNode }) {
  const [client, setClient] = useState<Client | null>(null);
  const [status, setStatus] = useState<Status>('disconnected');
  const [error, setError] = useState('');

  const connect = useCallback(async (url: string, token?: string) => {
    setClient((c) => { c?.close(); return null; });
    setStatus('connecting');
    setError('');
    try {
      const c = await Client.dial(url, token ? { token } : {});
      setClient(c);
      setStatus('connected');
    } catch (e) {
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

  const value = useMemo<ClientCtx>(
    () => ({ client, status, error, connect, disconnect }),
    [client, status, error, connect, disconnect],
  );
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useClient(): ClientCtx {
  const v = useContext(Ctx);
  if (!v) throw new Error('useClient must be inside <ClientProvider>');
  return v;
}
