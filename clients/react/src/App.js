import { jsx as _jsx, jsxs as _jsxs } from "react/jsx-runtime";
import { ClientProvider } from './hooks/useClient';
import { ConnectCard } from './components/ConnectCard';
import { SubscribeCard } from './components/SubscribeCard';
import { PublishCard } from './components/PublishCard';
import { FieldsCard } from './components/FieldsCard';
import { EntityListWidget } from './components/EntityListWidget';
import { NotificationWidget } from './components/NotificationWidget';
export function App() {
    return (_jsxs(ClientProvider, { children: [_jsxs("header", { children: [_jsx("h1", { children: "event_watch \u2014 React demo" }), _jsx("div", { className: "hint", children: "Full parity with the Wails app \u00B7 full integration widgets below" })] }), _jsxs("main", { className: "grid", children: [_jsx(ConnectCard, {}), _jsx(SubscribeCard, {}), _jsx(PublishCard, {}), _jsx(FieldsCard, {}), _jsx(EntityListWidget, {}), _jsx(NotificationWidget, {})] })] }));
}
