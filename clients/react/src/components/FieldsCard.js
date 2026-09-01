import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { useState } from 'react';
import { useClient } from '../hooks/useClient';
import { useSubscribedState } from '../hooks/useSubscribedState';
export function FieldsCard() {
    const { client } = useClient();
    const [topic, setTopic] = useState('int/hits');
    const [value, setValue] = useState('42');
    const [delta, setDelta] = useState('1');
    const state = useSubscribedState(topic);
    const typeOf = topic.split('/')[0] || '';
    const guarded = (fn) => async () => {
        if (!client)
            return alert('connect first');
        try {
            await fn();
        }
        catch (e) {
            alert(e.message);
        }
    };
    const doSet = guarded(async () => {
        if (typeOf === 'str')
            return client.stringField(topic).set(value);
        if (typeOf === 'int')
            return client.intField(topic).set(parseInt(value, 10) || 0);
        if (typeOf === 'time')
            return client.timeField(topic).set(value); // RFC3339 string
        throw new Error(`unsupported topic type "${typeOf}"; use str/, int/, or time/`);
    });
    const doIncr = guarded(async () => {
        if (typeOf !== 'int')
            throw new Error('incr only supported on int/');
        return client.intField(topic).incr(parseInt(delta, 10) || 1);
    });
    const doDecr = guarded(async () => {
        if (typeOf !== 'int')
            throw new Error('decr only supported on int/');
        return client.intField(topic).decr(parseInt(delta, 10) || 1);
    });
    const doNow = guarded(async () => {
        if (typeOf !== 'time')
            throw new Error('time-now only supported on time/');
        return client.timeField(topic).now();
    });
    const doDelete = guarded(async () => {
        if (typeOf === 'str')
            return client.stringField(topic).delete();
        if (typeOf === 'int')
            return client.intField(topic).delete();
        if (typeOf === 'time')
            return client.timeField(topic).delete();
        throw new Error('unsupported type');
    });
    return (_jsxs("section", { className: "card", children: [_jsx("h2", { children: "4. Fields (str / int / time)" }), _jsx("p", { className: "hint", children: "Topic prefix picks the type. Values are folded into a snapshot; every op is also a subscribable event." }), _jsxs("label", { children: ["Topic", _jsx("input", { value: topic, onChange: (e) => setTopic(e.target.value), autoCapitalize: "off", autoCorrect: "off", spellCheck: false })] }), _jsxs("label", { children: ["Value (for Set)", _jsx("input", { value: value, onChange: (e) => setValue(e.target.value), autoCapitalize: "off", autoCorrect: "off", spellCheck: false })] }), _jsxs("label", { children: ["Delta (for Incr/Decr)", _jsx("input", { value: delta, onChange: (e) => setDelta(e.target.value), autoCapitalize: "off", autoCorrect: "off", spellCheck: false })] }), _jsxs("div", { className: "row", children: [_jsx("button", { onClick: doSet, children: "Set" }), _jsx("button", { onClick: doIncr, children: "Incr" }), _jsx("button", { onClick: doDecr, children: "Decr" }), _jsx("button", { onClick: doNow, children: "Time-now" }), _jsx("button", { onClick: doDelete, children: "Delete" })] }), _jsxs("h3", { children: ["Current value ", _jsx("span", { className: "hint", children: "(live)" })] }), _jsx("pre", { children: state ? renderValue(typeOf, state) : '(none)' })] }));
}
function renderValue(kind, s) {
    const exists = s.exists ? 'set' : 'unset';
    if (kind === 'int')
        return `${s.value ?? 0}   (${exists})`;
    if (kind === 'str')
        return `${JSON.stringify(s.value)}   (${exists})`;
    if (kind === 'time')
        return `${s.value ?? '-'}   (${exists})`;
    return JSON.stringify(s, null, 2);
}
