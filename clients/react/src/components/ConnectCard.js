import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { useClient } from '../hooks/useClient';
export function ConnectCard() {
    const { status, error, connect, disconnect } = useClient();
    const [url, setUrl] = useState('ws://localhost:8080/ws');
    const [token, setToken] = useState('');
    const connected = status === 'connected';
    return (_jsxs("section", { className: "card", children: [_jsx("h2", { children: "1. Connect" }), _jsxs("label", { children: ["Server URL", _jsx("input", { value: url, onChange: (e) => setUrl(e.target.value), autoCapitalize: "off", autoCorrect: "off", spellCheck: false })] }), _jsxs("label", { children: ["Auth token (optional)", _jsx("input", { value: token, onChange: (e) => setToken(e.target.value), placeholder: "leave blank when --auth is off", autoCapitalize: "off", autoCorrect: "off", spellCheck: false })] }), _jsxs("div", { className: "row", children: [_jsx("button", { disabled: connected || status === 'connecting', onClick: () => connect(url, token || undefined), children: status === 'connecting' ? 'Connecting…' : 'Connect' }), _jsx("button", { disabled: !connected, onClick: disconnect, children: "Disconnect" }), _jsx("span", { className: `pill ${connected ? 'online' : 'offline'}`, children: status })] }), error && _jsxs("div", { className: "hint", style: { color: '#f6a' }, children: ["error: ", error] })] }));
}
