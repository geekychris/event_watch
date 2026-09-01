package io.eventwatch.client;

import com.fasterxml.jackson.annotation.JsonIgnoreProperties;
import com.fasterxml.jackson.databind.JsonNode;

/**
 * A single event delivered to a subscriber. Fields mirror the wire format;
 * {@link #state} is present on live fan-out and null on historical reads.
 */
@JsonIgnoreProperties(ignoreUnknown = true)
public final class Event {
    public String id;
    public String topic;
    public String type;
    public long seq;
    public String occurred_at;
    public String actor;
    public JsonNode payload;
    public JsonNode state;
}
