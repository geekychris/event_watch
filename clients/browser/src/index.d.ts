// Type declarations for @eventwatch/browser.
// Kept as .d.ts (not TypeScript source) so the runtime is plain JS and there
// is no build step. If you don't use TypeScript you can ignore this file.

export interface Event {
  id: string;
  topic: string;
  type: string;
  seq: number;
  occurred_at: string;
  actor?: string;
  payload?: unknown;
  /** Present only on live fan-out; absent on historical reads. */
  state?: unknown;
}

export type EventCallback = (event: Event) => void;

export interface Handle {
  close(): void;
}

export interface DialOpts {
  token?: string;
  /** Override the WebSocket constructor (mainly for tests). */
  websocket?: typeof WebSocket;
  backoffInitialMs?: number;
  backoffMaxMs?: number;
}

export interface SubscribeOpts {
  /** "latest" | "last:N" | "seq:N" */
  from?: string;
}

export class Client {
  static dial(url: string, opts?: DialOpts): Promise<Client>;
  subscribe(topic: string, cb: EventCallback, opts?: SubscribeOpts): Handle;
  publish(topic: string, type: string, payload?: unknown): Promise<number>;
  getState(topic: string): Promise<unknown | null>;
  stringField(topic: string): StringField;
  intField(topic: string): IntField;
  timeField(topic: string): TimeField;
  close(): void;
}

export class StringField {
  readonly topic: string;
  set(value: string): Promise<number>;
  delete(): Promise<number>;
  get(): Promise<[string, boolean]>;
}

export class IntField {
  readonly topic: string;
  set(value: number): Promise<number>;
  incr(delta?: number): Promise<number>;
  decr(delta?: number): Promise<number>;
  delete(): Promise<number>;
  get(): Promise<[number, boolean]>;
}

export class TimeField {
  readonly topic: string;
  set(when: Date | string): Promise<number>;
  now(): Promise<number>;
  add(seconds: number): Promise<number>;
  delete(): Promise<number>;
  get(): Promise<[Date | null, boolean]>;
}
