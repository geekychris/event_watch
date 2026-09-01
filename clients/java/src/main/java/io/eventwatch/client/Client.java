package io.eventwatch.client;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.node.ObjectNode;

import java.net.URI;
import java.net.URISyntaxException;
import java.net.http.HttpClient;
import java.net.http.WebSocket;
import java.nio.ByteBuffer;
import java.util.HashMap;
import java.util.Map;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;
import java.util.concurrent.atomic.AtomicLong;
import java.util.function.Consumer;

/**
 * event_watch WebSocket client. Uses Java 11's built-in {@link WebSocket} —
 * no third-party WS dependency. Subscribe returns a {@link Handle}; multiple
 * callbacks on the same topic share one upstream subscription and unsubscribe
 * only when the last handle closes.
 */
public final class Client implements AutoCloseable {

    private final URI url;
    private final String token;
    private final ObjectMapper json = new ObjectMapper();

    private volatile WebSocket ws;
    private final AtomicLong nextId = new AtomicLong(0);
    private final AtomicLong nextReq = new AtomicLong(0);
    private final Map<String, TopicSub> topics = new ConcurrentHashMap<>();
    private final Map<String, CompletableFuture<JsonNode>> pending = new ConcurrentHashMap<>();
    private final StringBuilder frameBuf = new StringBuilder();
    private volatile boolean closed = false;

    private static final class TopicSub {
        final String fromKind; // "latest" | "last:N" | "seq:N"
        final Map<Long, Consumer<Event>> callbacks = new ConcurrentHashMap<>();
        volatile long lastSeenSeq = 0;

        TopicSub(String fromKind) { this.fromKind = fromKind; }
    }

    private Client(URI url, String token) {
        this.url = url;
        this.token = token;
    }

    /** Dial the WS server. Blocks until the initial handshake completes. */
    public static Client dial(String url, String token) throws Exception {
        URI u;
        try { u = new URI(url); } catch (URISyntaxException e) { throw new IllegalArgumentException(e); }
        if (token != null && !token.isEmpty()) {
            String sep = (u.getQuery() == null || u.getQuery().isEmpty()) ? "?" : "&";
            u = new URI(url + sep + "access_token=" + token);
        }
        Client c = new Client(u, token == null ? "" : token);
        HttpClient http = HttpClient.newHttpClient();
        WebSocket.Builder b = http.newWebSocketBuilder();
        if (token != null && !token.isEmpty()) b.header("Authorization", "Bearer " + token);
        c.ws = b.buildAsync(c.url, c.new Listener()).get(10, TimeUnit.SECONDS);
        return c;
    }

    public static Client dial(String url) throws Exception { return dial(url, ""); }

    /** Close the underlying WebSocket. */
    @Override public void close() {
        closed = true;
        if (ws != null) ws.sendClose(WebSocket.NORMAL_CLOSURE, "bye");
    }

    // -- subscribe --

    /**
     * Register cb for events on topic. Returns a Handle; call close() to
     * remove. First cb opens the upstream sub; further cbs share it.
     */
    public Handle subscribe(String topic, Consumer<Event> cb) { return subscribe(topic, "latest", cb); }

    public Handle subscribe(String topic, String fromKind, Consumer<Event> cb) {
        long id = nextId.incrementAndGet();
        TopicSub ts = topics.computeIfAbsent(topic, k -> {
            TopicSub s = new TopicSub(fromKind);
            sendFrame(subscribeFrame(topic, fromKind, 0));
            return s;
        });
        ts.callbacks.put(id, cb);
        return new Handle(this, topic, id);
    }

    void removeCallback(String topic, long id) {
        TopicSub ts = topics.get(topic);
        if (ts == null) return;
        ts.callbacks.remove(id);
        if (ts.callbacks.isEmpty()) {
            topics.remove(topic);
            ObjectNode f = json.createObjectNode();
            f.put("op", "unsubscribe");
            f.put("topic", topic);
            sendFrame(f);
        }
    }

    // -- publish + state --

    public long publish(String topic, String type, Object payload) throws Exception {
        ObjectNode f = json.createObjectNode();
        f.put("op", "publish");
        f.put("topic", topic);
        f.put("type", type);
        if (payload != null) f.set("payload", json.valueToTree(payload));
        JsonNode resp = request(f);
        return resp.hasNonNull("last_seq") ? resp.get("last_seq").asLong() : 0;
    }

    /** Returns the current state as a JsonNode, or null if the topic has no state yet. */
    public JsonNode getState(String topic) throws Exception {
        ObjectNode f = json.createObjectNode();
        f.put("op", "get_state");
        f.put("topic", topic);
        try {
            JsonNode resp = request(f);
            return resp.get("state");
        } catch (RuntimeException e) {
            if (e.getMessage() != null && e.getMessage().toLowerCase().contains("not found")) return null;
            throw e;
        }
    }

    // -- typed field helpers --

    public StringField stringField(String topic) { return new StringField(this, topic); }
    public IntField intField(String topic)       { return new IntField(this, topic); }
    public TimeField timeField(String topic)     { return new TimeField(this, topic); }

    // -- internal --

    private ObjectNode subscribeFrame(String topic, String fromKind, long resumeSeq) {
        ObjectNode f = json.createObjectNode();
        f.put("op", "subscribe");
        f.put("topic", topic);
        if (resumeSeq > 0) f.put("from_seq", resumeSeq);
        else if (fromKind.startsWith("last:") || fromKind.startsWith("seq:")) f.put("from", fromKind);
        else f.put("from", "latest");
        return f;
    }

    private void sendFrame(ObjectNode f) {
        try {
            String s = json.writeValueAsString(f);
            ws.sendText(s, true);
        } catch (Exception e) {
            throw new RuntimeException(e);
        }
    }

    private JsonNode request(ObjectNode f) throws Exception {
        long n = nextReq.incrementAndGet();
        String id = "r" + n;
        f.put("req_id", id);
        CompletableFuture<JsonNode> fut = new CompletableFuture<>();
        pending.put(id, fut);
        sendFrame(f);
        try {
            return fut.get(10, TimeUnit.SECONDS);
        } catch (ExecutionException ee) {
            Throwable cause = ee.getCause();
            if (cause instanceof RuntimeException) throw (RuntimeException) cause;
            throw new RuntimeException(cause);
        } catch (TimeoutException te) {
            pending.remove(id);
            throw te;
        }
    }

    private void dispatch(JsonNode frame) {
        JsonNode typeN = frame.get("type");
        if (typeN == null) return;
        String type = typeN.asText();
        if ("event".equals(type)) {
            JsonNode evN = frame.get("event");
            if (evN == null || evN.isNull()) return;
            Event ev;
            try { ev = json.treeToValue(evN, Event.class); } catch (Exception e) { return; }
            TopicSub ts = topics.get(ev.topic);
            if (ts == null) return;
            if (ev.seq > ts.lastSeenSeq) ts.lastSeenSeq = ev.seq;
            for (Consumer<Event> cb : ts.callbacks.values()) cb.accept(ev);
        } else if ("state".equals(type) || "ack".equals(type) || "error".equals(type)) {
            JsonNode rid = frame.get("req_id");
            if (rid == null || rid.isNull()) return;
            CompletableFuture<JsonNode> fut = pending.remove(rid.asText());
            if (fut == null) return;
            if ("error".equals(type)) {
                fut.completeExceptionally(new RuntimeException(frame.hasNonNull("message") ? frame.get("message").asText() : "error"));
            } else {
                fut.complete(frame);
            }
        }
    }

    // Listener assembles multi-frame WS text messages before parsing.
    private final class Listener implements WebSocket.Listener {
        @Override
        public CompletionStage<?> onText(WebSocket ws, CharSequence data, boolean last) {
            frameBuf.append(data);
            if (last) {
                String s = frameBuf.toString();
                frameBuf.setLength(0);
                try { dispatch(json.readTree(s)); } catch (Exception ignored) {}
            }
            ws.request(1);
            return null;
        }

        @Override
        public CompletionStage<?> onBinary(WebSocket ws, ByteBuffer data, boolean last) {
            ws.request(1);
            return null;
        }

        @Override
        public CompletionStage<?> onClose(WebSocket ws, int statusCode, String reason) {
            return null;
        }

        @Override
        public void onError(WebSocket ws, Throwable error) {}
    }

    // Avoid pulling the whole java.util.concurrent.Flow surface in for one type name.
    private interface CompletionStage<T> extends java.util.concurrent.CompletionStage<T> {}
}
