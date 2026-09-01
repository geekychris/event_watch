import { jsx as _jsx, jsxs as _jsxs, Fragment as _Fragment } from "react/jsx-runtime";
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
        if (n.lastAt === null)
            return;
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
    return (_jsxs("section", { className: "card", children: [_jsx("h2", { children: "Widget 2 \u2014 Notification (change-data-capture)" }), _jsx("p", { className: "hint", children: "A minimal \"just tell me when something happens\" pattern. This widget ignores the event's payload and state \u2014 it only counts occurrences and flashes on arrival. Perfect for cache invalidation, \"please refresh me\" triggers, or lit-up-when-active status indicators." }), _jsxs("label", { children: ["Watch topic", _jsx("input", { value: topic, onChange: (e) => setTopic(e.target.value), autoCapitalize: "off", autoCorrect: "off", spellCheck: false, placeholder: "chat/notifications" })] }), _jsxs("div", { className: `notif-box ${flash ? 'flash' : ''}`, children: [_jsx("div", { className: "notif-count", children: n.count }), _jsxs("div", { className: "notif-label", children: ["notification", n.count === 1 ? '' : 's', " received"] }), _jsx("div", { className: "hint", style: { marginTop: 6 }, children: n.lastEventType ? _jsxs(_Fragment, { children: ["Last: ", _jsx("code", { children: n.lastEventType }), " \u00B7 ", ago] }) : 'waiting…' })] }), _jsxs("p", { className: "hint", style: { marginTop: 10 }, children: [_jsx("b", { children: "To test:" }), " use the Publish card (or any other client) to publish any event to ", _jsx("code", { children: topic || '<topic>' }), ". The panel will pulse and the counter tick."] })] }));
}
function relative(ms) {
    if (ms < 1000)
        return 'just now';
    if (ms < 60_000)
        return `${Math.floor(ms / 1000)}s ago`;
    if (ms < 3_600_000)
        return `${Math.floor(ms / 60_000)}m ago`;
    return `${Math.floor(ms / 3_600_000)}h ago`;
}
