import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
// EntityListWidget — the "deep integration" example.
//
// Pattern: your app holds a list of entities the user cares about (PRs,
// deploys, jobs, ...). Each row corresponds to one topic. Adding a row
// starts a subscription for that topic; the row renders the reduced state
// (title, status, counters, ...) and updates in place as events arrive.
// Removing a row unsubscribes.
//
// This is what you build a "dashboard of things" out of — the client
// library gives you both the current value (via GetState) AND live updates
// (via the state field on subscribed events) in one hook.
import { useState } from 'react';
import { useClient } from '../hooks/useClient';
import { useSubscribedState } from '../hooks/useSubscribedState';
export function EntityListWidget() {
    const { client } = useClient();
    const [nextTopic, setNextTopic] = useState('pr/octo/hello/1');
    const [items, setItems] = useState([]);
    const add = () => {
        const t = nextTopic.trim();
        if (!t)
            return;
        if (items.includes(t))
            return alert('already in the list');
        setItems((xs) => [...xs, t]);
    };
    const remove = (t) => setItems((xs) => xs.filter((x) => x !== t));
    return (_jsxs("section", { className: "card", children: [_jsx("h2", { children: "Widget 1 \u2014 Entity list (deep integration)" }), _jsxs("p", { className: "hint", children: ["Add a topic; that row subscribes and shows the current reduced state, auto-updating on every event. This is how you build a \"dashboard of things\" \u2014 one subscription per row, one server round-trip for the initial value, then live updates via ", _jsx("code", { children: "event.state" }), "."] }), _jsxs("label", { children: ["Topic", _jsx("input", { value: nextTopic, onChange: (e) => setNextTopic(e.target.value), autoCapitalize: "off", autoCorrect: "off", spellCheck: false, placeholder: "pr/octo/hello/1" })] }), _jsxs("div", { className: "row", children: [_jsx("button", { onClick: add, disabled: !client, children: "Add to list" }), _jsx("span", { className: "hint", children: client ? `${items.length} tracked` : '(connect first)' })] }), _jsxs("table", { className: "entity-table", children: [_jsx("thead", { children: _jsxs("tr", { children: [_jsx("th", { children: "Topic" }), _jsx("th", { children: "Title / author" }), _jsx("th", { children: "State" }), _jsx("th", { children: "Signals" }), _jsx("th", { children: "Updated" }), _jsx("th", {})] }) }), _jsxs("tbody", { children: [items.map((t) => _jsx(EntityRow, { topic: t, onRemove: () => remove(t) }, t)), items.length === 0 && (_jsx("tr", { children: _jsx("td", { colSpan: 6, className: "hint", children: "(no entities \u2014 add a topic above; then use another client to publish events for that topic and watch the row update)" }) }))] })] })] }));
}
function EntityRow({ topic, onRemove }) {
    // The magic line: subscribes for the row's lifetime, returns latest state.
    const state = useSubscribedState(topic);
    const s = state || {};
    return (_jsxs("tr", { children: [_jsx("td", { children: _jsx("code", { children: topic }) }), _jsx("td", { children: s.title ? _jsxs(_Fragment, { children: [_jsx("b", { children: s.title }), _jsx("br", {}), _jsxs("span", { className: "hint", children: ["by ", s.author || '—'] })] }) : _jsx("span", { className: "hint", children: "(no title yet)" }) }), _jsx("td", { children: _jsx("span", { className: `badge state-${s.state || 'unknown'}`, children: s.state || '—' }) }), _jsxs("td", { className: "hint", children: ["\u2713 ", s.approvals ?? 0, s.reviewers?.length ? _jsxs(_Fragment, { children: [" \u00B7 \uD83D\uDC40 ", s.reviewers.length] }) : null, s.comments ? _jsxs(_Fragment, { children: [" \u00B7 \uD83D\uDCAC ", s.comments] }) : null, s.checks && ((s.checks.passed ?? 0) + (s.checks.failed ?? 0) + (s.checks.pending ?? 0) > 0) && (_jsxs(_Fragment, { children: [" \u00B7 ", _jsxs("span", { style: { color: '#7fd' }, children: [s.checks.passed ?? 0, " ok"] }), ' / ', _jsxs("span", { style: { color: '#f6a' }, children: [s.checks.failed ?? 0, " fail"] }), ' / ', s.checks.pending ?? 0, " pending"] }))] }), _jsx("td", { className: "hint", children: s.updated_at ? relative(s.updated_at) : '—' }), _jsx("td", { children: _jsx("button", { onClick: onRemove, children: "\u00D7" }) })] }));
}
function relative(iso) {
    const ms = Date.now() - new Date(iso).getTime();
    if (Number.isNaN(ms))
        return iso;
    if (ms < 1000)
        return 'just now';
    if (ms < 60_000)
        return `${Math.floor(ms / 1000)}s ago`;
    if (ms < 3_600_000)
        return `${Math.floor(ms / 60_000)}m ago`;
    return `${Math.floor(ms / 3_600_000)}h ago`;
}
