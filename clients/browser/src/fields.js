// Typed field helpers — thin sugar over Client.publish + Client.getState.
// Nothing here uses server routes that don't already exist; the wrappers
// just fix the event type + payload shape so callers can't get them wrong.

export class StringField {
  constructor(client, topic) { this._c = client; this.topic = topic; }
  set(value)   { return this._c.publish(this.topic, 'str_set', { value }); }
  delete()     { return this._c.publish(this.topic, 'str_delete'); }
  async get() {
    const s = await this._c.getState(this.topic);
    if (!s) return ['', false];
    return [String(s.value ?? ''), Boolean(s.exists)];
  }
}

export class IntField {
  constructor(client, topic) { this._c = client; this.topic = topic; }
  set(value)          { return this._c.publish(this.topic, 'int_set', { value }); }
  incr(delta = 1)     { return this._c.publish(this.topic, 'int_incr', { delta }); }
  decr(delta = 1)     { return this._c.publish(this.topic, 'int_decr', { delta }); }
  delete()            { return this._c.publish(this.topic, 'int_delete'); }
  async get() {
    const s = await this._c.getState(this.topic);
    if (!s) return [0, false];
    return [Number(s.value ?? 0), Boolean(s.exists)];
  }
}

export class TimeField {
  constructor(client, topic) { this._c = client; this.topic = topic; }
  /** @param {Date|string} when — a Date, or an RFC3339 string. */
  set(when) {
    const value = when instanceof Date ? when.toISOString() : String(when);
    return this._c.publish(this.topic, 'time_set', { value });
  }
  now()               { return this._c.publish(this.topic, 'time_now'); }
  add(seconds)        { return this._c.publish(this.topic, 'time_add', { seconds }); }
  delete()            { return this._c.publish(this.topic, 'time_delete'); }
  /** @returns {[Date|null, boolean]} */
  async get() {
    const s = await this._c.getState(this.topic);
    if (!s) return [null, false];
    const exists = Boolean(s.exists);
    if (!s.value) return [null, exists];
    const d = new Date(s.value);
    return [isNaN(d.getTime()) ? null : d, exists];
  }
}
