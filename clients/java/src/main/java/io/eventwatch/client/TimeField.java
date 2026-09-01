package io.eventwatch.client;

import com.fasterxml.jackson.databind.JsonNode;

import java.time.Instant;
import java.util.Map;

public final class TimeField {
    private final Client c;
    public final String topic;

    TimeField(Client c, String topic) { this.c = c; this.topic = topic; }

    public long set(Instant when) throws Exception {
        return c.publish(topic, "time_set", Map.of("value", when.toString()));
    }
    public long now() throws Exception    { return c.publish(topic, "time_now", null); }
    public long add(long secs) throws Exception { return c.publish(topic, "time_add", Map.of("seconds", secs)); }
    public long delete() throws Exception { return c.publish(topic, "time_delete", null); }

    /** Result[0]=Instant value or null, Result[1]=Boolean exists. */
    public Object[] get() throws Exception {
        JsonNode s = c.getState(topic);
        if (s == null) return new Object[]{null, Boolean.FALSE};
        boolean exists = s.hasNonNull("exists") && s.get("exists").asBoolean(false);
        Instant when = null;
        if (s.hasNonNull("value")) {
            try { when = Instant.parse(s.get("value").asText()); } catch (Exception ignored) {}
        }
        return new Object[]{when, exists};
    }
}
