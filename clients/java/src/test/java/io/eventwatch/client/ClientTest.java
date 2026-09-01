package io.eventwatch.client;

import com.fasterxml.jackson.databind.JsonNode;
import org.junit.jupiter.api.Test;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

import static org.junit.jupiter.api.Assertions.*;

/**
 * Integration tests. Require a running event_watch server on :8080.
 * Skipped silently if the server isn't reachable so unit runs don't fail.
 */
class ClientTest {
    private static final String URL = System.getenv().getOrDefault("EW_URL", "ws://localhost:8080/ws");

    private Client dialOrSkip() throws Exception {
        try { return Client.dial(URL); }
        catch (Exception e) {
            System.err.println("event_watch server not reachable at " + URL + " — skipping IT");
            return null;
        }
    }

    @Test void intFieldSetIncrDecrGet() throws Exception {
        Client c = dialOrSkip();
        if (c == null) return;
        try {
            IntField f = c.intField("int/javatest/counter");
            f.set(100);
            f.incr(5);
            f.incr();       // default +1
            f.decr(4);
            Object[] g = f.get();
            assertEquals(102L, ((Long) g[0]).longValue());
            assertEquals(Boolean.TRUE, g[1]);
        } finally { c.close(); }
    }

    @Test void stringFieldSetDeleteGet() throws Exception {
        Client c = dialOrSkip();
        if (c == null) return;
        try {
            StringField f = c.stringField("str/javatest/name");
            f.set("alice");
            Object[] g = f.get();
            assertEquals("alice", g[0]);
            assertEquals(Boolean.TRUE, g[1]);
            f.delete();
            Object[] g2 = f.get();
            assertEquals("", g2[0]);
            assertEquals(Boolean.FALSE, g2[1]);
        } finally { c.close(); }
    }

    @Test void subscribeReceivesEventsWithState() throws Exception {
        Client c = dialOrSkip();
        if (c == null) return;
        try {
            CountDownLatch got = new CountDownLatch(3);
            AtomicInteger lastValue = new AtomicInteger();
            try (Handle h = c.subscribe("int/javatest/live", ev -> {
                if (ev.state != null && ev.state.hasNonNull("value")) {
                    lastValue.set(ev.state.get("value").asInt());
                }
                got.countDown();
            })) {
                Thread.sleep(100); // let subscribe frame land
                IntField f = c.intField("int/javatest/live");
                f.set(10);
                f.incr(5);
                f.decr(2);
                assertTrue(got.await(3, TimeUnit.SECONDS), "did not receive 3 events");
                assertEquals(13, lastValue.get());
            }
        } finally { c.close(); }
    }
}
