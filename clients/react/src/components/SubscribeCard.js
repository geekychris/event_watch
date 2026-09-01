import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
// Raw event-feed subscriber. Same UX as the Wails app: type a topic, click
// Subscribe, see events stream in.
import { useEffect, useRef, useState } from 'react';
import { useClient } from '../hooks/useClient';
export function SubscribeCard() {
    const { client } = useClient();
    const [topic, setTopic] = useState('pr/octo/hello/1');
    const [from, setFrom] = useState('latest');
    const [events, setEvents] = useState([]);
    const handleRef = useRef(null);
    const feedRef = useRef(null);
    useEffect(() => {
        if (feedRef.current)
            feedRef.current.scrollTop = feedRef.current.scrollHeight;
    }, [events]);
    const subscribe = () => {
        if (!client)
            return alert('connect first');
        if (handleRef.current)
            handleRef.current.close();
        setEvents([]);
        handleRef.current = client.subscribe(topic, (ev) => setEvents((prev) => [...prev, ev].slice(-500)), { from });
    };
    const unsubscribe = () => {
        if (handleRef.current) {
            handleRef.current.close();
            handleRef.current = null;
        }
    };
    useEffect(() => () => handleRef.current?.close(), []);
    const badge = (t) => {
        const kind = t.indexOf('/') > 0 ? t.slice(0, t.indexOf('/')) : '';
        return _jsx("span", { className: `badge ${kind}`, children: /* placeholder — filled below */ null });
    };
    return (_jsxs("section", { className: "card", children: [_jsx("h2", { children: "2. Subscribe" }), _jsxs("label", { children: ["Topic", _jsx("input", { value: topic, onChange: (e) => setTopic(e.target.value), autoCapitalize: "off", autoCorrect: "off", spellCheck: false })] }), _jsxs("label", { children: ["From", _jsxs("select", { value: from, onChange: (e) => setFrom(e.target.value), children: [_jsx("option", { value: "latest", children: "latest (new only)" }), _jsx("option", { value: "last:10", children: "last 10" }), _jsx("option", { value: "last:50", children: "last 50" }), _jsx("option", { value: "seq:1", children: "from seq 1" })] })] }), _jsxs("div", { className: "row", children: [_jsx("button", { onClick: subscribe, children: "Subscribe" }), _jsx("button", { onClick: unsubscribe, children: "Unsubscribe" })] }), _jsx("h3", { children: "Live events" }), _jsx("div", { className: "feed", ref: feedRef, children: events.map((e, i) => (_jsxs("div", { className: "ev", children: [_jsx("span", { className: `badge ${e.topic.split('/')[0] || ''}`, children: e.type }), _jsx("code", { children: e.topic }), " seq=", e.seq, e.payload !== undefined && _jsxs("code", { children: [" ", JSON.stringify(e.payload)] }), e.state !== undefined && (_jsxs(_Fragment, { children: [" \u2192 ", _jsx("b", { children: summarise(e.topic, e.state) })] }))] }, i))) }), badge('') && null] }));
}
function summarise(topic, state) {
    const kind = topic.split('/')[0] || '';
    if (typeof state !== 'object' || state === null)
        return String(state);
    const s = state;
    if (kind === 'int' && typeof s.value === 'number')
        return String(s.value);
    if (kind === 'str' && 'value' in s)
        return JSON.stringify(s.value);
    if (kind === 'time' && s.value)
        return String(s.value);
    return JSON.stringify(s);
}
