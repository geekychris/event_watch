// event_watch client for ESP-IDF. Small surface: connect, subscribe with a
// callback, publish, int/str field helpers. Intended for MCU workloads that
// push sensor values or subscribe to command topics.
//
// This is NOT full parity with the Go/Java/Python/Rust clients — it does not
// support refcounted local dispatch, request/response over WS, or per-topic
// resume seq on reconnect. It DOES auto-reconnect and re-issue subscribes.
#ifndef EVENTWATCH_H
#define EVENTWATCH_H

#include <stdbool.h>
#include <stdint.h>
#include <stddef.h>

#ifdef __cplusplus
extern "C" {
#endif

typedef struct eventwatch_client eventwatch_client_t;

// Callback fired for each event on a subscribed topic. `payload_json` and
// `state_json` are borrowed cJSON-formatted strings — copy them if needed
// beyond the callback's return.
typedef void (*eventwatch_event_cb)(const char *topic, const char *type,
                                    uint64_t seq, const char *payload_json,
                                    const char *state_json, void *user);

typedef struct {
    const char *url;             // ws://host:port/ws
    const char *auth_token;      // optional bearer token; NULL/"" for none
    eventwatch_event_cb event_cb;
    void *user;
} eventwatch_config_t;

// Start the client. Returns NULL on config error. The client owns its
// own task and reconnects on drop.
eventwatch_client_t *eventwatch_start(const eventwatch_config_t *cfg);

// Stop and free the client.
void eventwatch_stop(eventwatch_client_t *c);

// Subscribe to a topic. `from` = "latest" or "last:N" or "seq:N".
// Idempotent per (client, topic) — a repeat is a no-op.
void eventwatch_subscribe(eventwatch_client_t *c, const char *topic, const char *from);

void eventwatch_unsubscribe(eventwatch_client_t *c, const char *topic);

// Publish a raw event.
void eventwatch_publish_json(eventwatch_client_t *c, const char *topic,
                             const char *type, const char *payload_json);

// Field helpers. All fire-and-forget — no server ack. To read the current
// value, subscribe and use the `state_json` in the callback.
void eventwatch_str_set(eventwatch_client_t *c, const char *topic, const char *value);
void eventwatch_str_delete(eventwatch_client_t *c, const char *topic);
void eventwatch_int_set(eventwatch_client_t *c, const char *topic, int64_t value);
void eventwatch_int_incr(eventwatch_client_t *c, const char *topic, int64_t delta);
void eventwatch_int_decr(eventwatch_client_t *c, const char *topic, int64_t delta);
void eventwatch_int_delete(eventwatch_client_t *c, const char *topic);
void eventwatch_time_now(eventwatch_client_t *c, const char *topic);

#ifdef __cplusplus
}
#endif

#endif  // EVENTWATCH_H
