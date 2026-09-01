import { ClientProvider } from './hooks/useClient';
import { ConnectCard } from './components/ConnectCard';
import { SubscribeCard } from './components/SubscribeCard';
import { PublishCard } from './components/PublishCard';
import { FieldsCard } from './components/FieldsCard';
import { EntityListWidget } from './components/EntityListWidget';
import { NotificationWidget } from './components/NotificationWidget';

export function App() {
  return (
    <ClientProvider>
      <header>
        <h1>event_watch — React demo</h1>
        <div className="hint">Full parity with the Wails app · full integration widgets below</div>
      </header>
      <main className="grid">
        <ConnectCard />
        <SubscribeCard />
        <PublishCard />
        <FieldsCard />
        <EntityListWidget />
        <NotificationWidget />
      </main>
    </ClientProvider>
  );
}
