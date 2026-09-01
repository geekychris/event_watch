package io.eventwatch.client;

import com.fasterxml.jackson.databind.JsonNode;

import java.util.Map;

public final class StringField {
    private final Client c;
    public final String topic;

    StringField(Client c, String topic) { this.c = c; this.topic = topic; }

    public long set(String value) throws Exception {
        return c.publish(topic, "str_set", Map.of("value", value));
    }

    public long delete() throws Exception { return c.publish(topic, "str_delete", null); }

    /** Result[0]=value (may be ""), Result[1]=Boolean exists. */
    public Object[] get() throws Exception {
        JsonNode s = c.getState(topic);
        if (s == null) return new Object[]{"", Boolean.FALSE};
        String v = s.hasNonNull("value") ? s.get("value").asText("") : "";
        boolean exists = s.hasNonNull("exists") && s.get("exists").asBoolean(false);
        return new Object[]{v, exists};
    }
}
