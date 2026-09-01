import { useState } from 'react';
import { useClient } from '../hooks/useClient';

export function ConnectCard() {
  const { status, error, connect, disconnect } = useClient();
  const [url, setUrl] = useState('ws://localhost:8080/ws');
  const [token, setToken] = useState('');

  const connected = status === 'connected';
  return (
    <section className="card">
      <h2>1. Connect</h2>
      <label>Server URL
        <input value={url} onChange={(e) => setUrl(e.target.value)}
               autoCapitalize="off" autoCorrect="off" spellCheck={false} />
      </label>
      <label>Auth token (optional)
        <input value={token} onChange={(e) => setToken(e.target.value)}
               placeholder="leave blank when --auth is off"
               autoCapitalize="off" autoCorrect="off" spellCheck={false} />
      </label>
      <div className="row">
        <button disabled={connected || status === 'connecting'}
                onClick={() => connect(url, token || undefined)}>
          {status === 'connecting' ? 'Connecting…' : 'Connect'}
        </button>
        <button disabled={!connected} onClick={disconnect}>Disconnect</button>
        <span className={`pill ${connected ? 'online' : 'offline'}`}>{status}</span>
      </div>
      {error && <div className="hint" style={{color: '#f6a'}}>error: {error}</div>}
    </section>
  );
}
