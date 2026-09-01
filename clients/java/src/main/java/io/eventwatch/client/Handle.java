package io.eventwatch.client;

/**
 * Handle to one callback registration. Close removes the callback; when the
 * last handle for a topic closes, the client sends an unsubscribe upstream.
 */
public final class Handle implements AutoCloseable {
    private final Client client;
    private final String topic;
    private final long id;
    private volatile boolean closed = false;

    Handle(Client client, String topic, long id) {
        this.client = client;
        this.topic = topic;
        this.id = id;
    }

    @Override
    public void close() {
        if (closed) return;
        closed = true;
        client.removeCallback(topic, id);
    }
}
