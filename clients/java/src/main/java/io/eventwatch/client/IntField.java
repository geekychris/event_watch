package io.eventwatch.client;

import com.fasterxml.jackson.databind.JsonNode;

import java.util.Map;

public final class IntField {
    private final Client c;
    public final String topic;

    IntField(Client c, String topic) { this.c = c; this.topic = topic; }

    public long set(long value) throws Exception  { return c.publish(topic, "int_set",  Map.of("value", value)); }
    public long incr(long delta) throws Exception { return c.publish(topic, "int_incr", Map.of("delta", delta)); }
    public long incr() throws Exception           { return incr(1); }
    public long decr(long delta) throws Exception { return c.publish(topic, "int_decr", Map.of("delta", delta)); }
    public long decr() throws Exception           { return decr(1); }
    public long delete() throws Exception         { return c.publish(topic, "int_delete", null); }

    /** Result[0]=Long value, Result[1]=Boolean exists. Value is 0 if unset. */
    public Object[] get() throws Exception {
        JsonNode s = c.getState(topic);
        if (s == null) return new Object[]{0L, Boolean.FALSE};
        long v = s.hasNonNull("value") ? s.get("value").asLong(0) : 0L;
        boolean exists = s.hasNonNull("exists") && s.get("exists").asBoolean(false);
        return new Object[]{v, exists};
    }
}
