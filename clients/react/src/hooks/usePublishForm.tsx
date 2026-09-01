// Small context so the CheatsheetCard can hand a canned publish to the
// PublishCard without prop-drilling. Both cards read/write the same
// {topic, type, payload} triple.
import { createContext, useContext, useMemo, useState, type ReactNode } from 'react';

interface PublishForm {
  topic: string;
  type: string;
  payload: string;                    // stored as JSON string (what the textarea holds)
  set(fields: { topic?: string; type?: string; payload?: unknown }): void;
}

const Ctx = createContext<PublishForm | null>(null);

export function PublishFormProvider({ children }: { children: ReactNode }) {
  const [topic, setTopic] = useState('chat/general');
  const [type, setType] = useState('msg_posted');
  const [payload, setPayload] = useState('{"user":"alice","text":"hi"}');

  const value = useMemo<PublishForm>(() => ({
    topic, type, payload,
    set: (f) => {
      if (f.topic !== undefined) setTopic(f.topic);
      if (f.type  !== undefined) setType(f.type);
      if (f.payload !== undefined) {
        setPayload(typeof f.payload === 'string' ? f.payload : JSON.stringify(f.payload));
      }
    },
  }), [topic, type, payload]);

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function usePublishForm(): PublishForm {
  const v = useContext(Ctx);
  if (!v) throw new Error('usePublishForm must be inside <PublishFormProvider>');
  return v;
}
