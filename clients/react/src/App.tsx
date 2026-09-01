import { ClientProvider } from './hooks/useClient';
import { PublishFormProvider } from './hooks/usePublishForm';
import { ConnectCard } from './components/ConnectCard';
import { SubscribeCard } from './components/SubscribeCard';
import { PublishCard } from './components/PublishCard';
import { FieldsCard } from './components/FieldsCard';
import { EntityListWidget } from './components/EntityListWidget';
import { NotificationWidget } from './components/NotificationWidget';
import { CheatsheetCard } from './components/CheatsheetCard';

export function App() {
  return (
    <ClientProvider>
      <PublishFormProvider>
        <header>
          <h1>event_watch — React demo</h1>
          <div className="hint">Full parity with the Wails app · deep + shallow integration widgets · in-UI cheatsheet with click-to-inject</div>
        </header>
        <main className="grid">
          <ConnectCard />
          <SubscribeCard />
          <PublishCard />
          <FieldsCard />
          <EntityListWidget />
          <NotificationWidget />
          <CheatsheetCard />
        </main>
      </PublishFormProvider>
    </ClientProvider>
  );
}
