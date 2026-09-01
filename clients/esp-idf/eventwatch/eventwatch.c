// event_watch client for ESP-IDF.
//
// Uses the ESP-IDF esp_websocket_client component and cJSON. Runs entirely
// on the WS client's callback thread — no extra task spawned.
#include "eventwatch.h"

#include <string.h>
#include <stdlib.h>
#include <stdio.h>

#include "esp_log.h"
#include "esp_websocket_client.h"
#include "cJSON.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"

static const char *TAG = "eventwatch";

#define EW_MAX_TOPICS 16

typedef struct {
    char *topic;
    char *from;      // "latest" | "last:N" | "seq:N"
    uint64_t last_seq;
} ew_topic_t;

struct eventwatch_client {
    esp_websocket_client_handle_t ws;
    eventwatch_event_cb cb;
    void *user;
    char *auth_token;                       // owned
    SemaphoreHandle_t topics_mtx;
    ew_topic_t topics[EW_MAX_TOPICS];
    int topic_count;
};

// -- helpers --

static void send_frame(eventwatch_client_t *c, const char *json) {
    if (!c || !c->ws || !json) return;
    esp_websocket_client_send_text(c->ws, json, (int)strlen(json), portMAX_DELAY);
}

// url_with_token strips trailing '\0's — return a static buffer with the
// access_token query appended when token is set. Caller must not free.
static const char *url_with_token(const char *url, const char *token) {
    static char buf[256];
    if (!token || !*token) {
        strncpy(buf, url, sizeof(buf) - 1);
        buf[sizeof(buf) - 1] = 0;
        return buf;
    }
    const char *sep = strchr(url, '?') ? "&" : "?";
    snprintf(buf, sizeof(buf), "%s%saccess_token=%s", url, sep, token);
    return buf;
}

// -- subscription bookkeeping --

static int find_topic(eventwatch_client_t *c, const char *topic) {
    for (int i = 0; i < c->topic_count; i++) {
        if (c->topics[i].topic && strcmp(c->topics[i].topic, topic) == 0) return i;
    }
    return -1;
}

static void resubscribe_all(eventwatch_client_t *c) {
    if (xSemaphoreTake(c->topics_mtx, portMAX_DELAY) != pdTRUE) return;
    for (int i = 0; i < c->topic_count; i++) {
        cJSON *root = cJSON_CreateObject();
        cJSON_AddStringToObject(root, "op", "subscribe");
        cJSON_AddStringToObject(root, "topic", c->topics[i].topic);
        if (c->topics[i].last_seq > 0) {
            cJSON_AddNumberToObject(root, "from_seq", (double)(c->topics[i].last_seq + 1));
        } else {
            cJSON_AddStringToObject(root, "from", c->topics[i].from);
        }
        char *s = cJSON_PrintUnformatted(root);
        send_frame(c, s);
        cJSON_free(s);
        cJSON_Delete(root);
    }
    xSemaphoreGive(c->topics_mtx);
}

// -- WS event handler --

static void ws_event_handler(void *arg, esp_event_base_t base, int32_t id, void *data) {
    eventwatch_client_t *c = (eventwatch_client_t *)arg;
    esp_websocket_event_data_t *ev = (esp_websocket_event_data_t *)data;

    switch (id) {
    case WEBSOCKET_EVENT_CONNECTED:
        ESP_LOGI(TAG, "connected");
        resubscribe_all(c);
        break;

    case WEBSOCKET_EVENT_DISCONNECTED:
        ESP_LOGW(TAG, "disconnected");
        break;

    case WEBSOCKET_EVENT_DATA: {
        if (ev->op_code != 0x01) break;         // 0x01 = text frame
        cJSON *root = cJSON_ParseWithLength(ev->data_ptr, ev->data_len);
        if (!root) break;
        const cJSON *type_n = cJSON_GetObjectItem(root, "type");
        if (cJSON_IsString(type_n) && strcmp(type_n->valuestring, "event") == 0) {
            const cJSON *evn = cJSON_GetObjectItem(root, "event");
            if (cJSON_IsObject(evn)) {
                const cJSON *topic_n   = cJSON_GetObjectItem(evn, "topic");
                const cJSON *etype_n   = cJSON_GetObjectItem(evn, "type");
                const cJSON *seq_n     = cJSON_GetObjectItem(evn, "seq");
                const cJSON *payload_n = cJSON_GetObjectItem(evn, "payload");
                const cJSON *state_n   = cJSON_GetObjectItem(evn, "state");
                uint64_t seq = cJSON_IsNumber(seq_n) ? (uint64_t)seq_n->valuedouble : 0;
                if (cJSON_IsString(topic_n)) {
                    // Update last_seen_seq for resume on reconnect.
                    if (xSemaphoreTake(c->topics_mtx, 0) == pdTRUE) {
                        int idx = find_topic(c, topic_n->valuestring);
                        if (idx >= 0 && seq > c->topics[idx].last_seq) {
                            c->topics[idx].last_seq = seq;
                        }
                        xSemaphoreGive(c->topics_mtx);
                    }
                    if (c->cb) {
                        char *p = payload_n ? cJSON_PrintUnformatted(payload_n) : NULL;
                        char *s = state_n   ? cJSON_PrintUnformatted(state_n)   : NULL;
                        c->cb(topic_n->valuestring,
                              cJSON_IsString(etype_n) ? etype_n->valuestring : "",
                              seq, p ? p : "", s ? s : "", c->user);
                        if (p) cJSON_free(p);
                        if (s) cJSON_free(s);
                    }
                }
            }
        }
        cJSON_Delete(root);
        break;
    }

    default:
        break;
    }
}

// -- public API --

eventwatch_client_t *eventwatch_start(const eventwatch_config_t *cfg) {
    if (!cfg || !cfg->url) return NULL;
    eventwatch_client_t *c = calloc(1, sizeof(*c));
    if (!c) return NULL;
    c->cb = cfg->event_cb;
    c->user = cfg->user;
    if (cfg->auth_token) c->auth_token = strdup(cfg->auth_token);
    c->topics_mtx = xSemaphoreCreateMutex();

    esp_websocket_client_config_t ws_cfg = {
        .uri = url_with_token(cfg->url, c->auth_token ? c->auth_token : ""),
        .reconnect_timeout_ms = 5000,
        .network_timeout_ms   = 10000,
    };
    c->ws = esp_websocket_client_init(&ws_cfg);
    esp_websocket_register_events(c->ws, WEBSOCKET_EVENT_ANY, ws_event_handler, c);
    esp_websocket_client_start(c->ws);
    return c;
}

void eventwatch_stop(eventwatch_client_t *c) {
    if (!c) return;
    esp_websocket_client_stop(c->ws);
    esp_websocket_client_destroy(c->ws);
    for (int i = 0; i < c->topic_count; i++) {
        free(c->topics[i].topic);
        free(c->topics[i].from);
    }
    if (c->topics_mtx) vSemaphoreDelete(c->topics_mtx);
    free(c->auth_token);
    free(c);
}

void eventwatch_subscribe(eventwatch_client_t *c, const char *topic, const char *from) {
    if (!c || !topic) return;
    xSemaphoreTake(c->topics_mtx, portMAX_DELAY);
    int idx = find_topic(c, topic);
    if (idx < 0 && c->topic_count < EW_MAX_TOPICS) {
        c->topics[c->topic_count].topic    = strdup(topic);
        c->topics[c->topic_count].from     = strdup(from && *from ? from : "latest");
        c->topics[c->topic_count].last_seq = 0;
        c->topic_count++;
    }
    xSemaphoreGive(c->topics_mtx);

    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "op", "subscribe");
    cJSON_AddStringToObject(root, "topic", topic);
    cJSON_AddStringToObject(root, "from", from && *from ? from : "latest");
    char *s = cJSON_PrintUnformatted(root);
    send_frame(c, s);
    cJSON_free(s);
    cJSON_Delete(root);
}

void eventwatch_unsubscribe(eventwatch_client_t *c, const char *topic) {
    if (!c || !topic) return;
    xSemaphoreTake(c->topics_mtx, portMAX_DELAY);
    int idx = find_topic(c, topic);
    if (idx >= 0) {
        free(c->topics[idx].topic);
        free(c->topics[idx].from);
        // compact
        for (int i = idx; i < c->topic_count - 1; i++) c->topics[i] = c->topics[i + 1];
        c->topic_count--;
    }
    xSemaphoreGive(c->topics_mtx);

    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "op", "unsubscribe");
    cJSON_AddStringToObject(root, "topic", topic);
    char *s = cJSON_PrintUnformatted(root);
    send_frame(c, s);
    cJSON_free(s);
    cJSON_Delete(root);
}

void eventwatch_publish_json(eventwatch_client_t *c, const char *topic,
                             const char *type, const char *payload_json) {
    if (!c || !topic || !type) return;
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "op", "publish");
    cJSON_AddStringToObject(root, "topic", topic);
    cJSON_AddStringToObject(root, "type", type);
    if (payload_json && *payload_json) {
        cJSON *p = cJSON_Parse(payload_json);
        if (p) cJSON_AddItemToObject(root, "payload", p);
    }
    char *s = cJSON_PrintUnformatted(root);
    send_frame(c, s);
    cJSON_free(s);
    cJSON_Delete(root);
}

// -- field helpers --

static void publish_typed(eventwatch_client_t *c, const char *topic,
                          const char *type, cJSON *payload) {
    cJSON *root = cJSON_CreateObject();
    cJSON_AddStringToObject(root, "op", "publish");
    cJSON_AddStringToObject(root, "topic", topic);
    cJSON_AddStringToObject(root, "type", type);
    if (payload) cJSON_AddItemToObject(root, "payload", payload);
    char *s = cJSON_PrintUnformatted(root);
    send_frame(c, s);
    cJSON_free(s);
    cJSON_Delete(root);
}

void eventwatch_str_set(eventwatch_client_t *c, const char *topic, const char *value) {
    cJSON *p = cJSON_CreateObject();
    cJSON_AddStringToObject(p, "value", value ? value : "");
    publish_typed(c, topic, "str_set", p);
}

void eventwatch_str_delete(eventwatch_client_t *c, const char *topic) {
    publish_typed(c, topic, "str_delete", NULL);
}

void eventwatch_int_set(eventwatch_client_t *c, const char *topic, int64_t value) {
    cJSON *p = cJSON_CreateObject();
    cJSON_AddNumberToObject(p, "value", (double)value);
    publish_typed(c, topic, "int_set", p);
}

void eventwatch_int_incr(eventwatch_client_t *c, const char *topic, int64_t delta) {
    cJSON *p = cJSON_CreateObject();
    cJSON_AddNumberToObject(p, "delta", (double)delta);
    publish_typed(c, topic, "int_incr", p);
}

void eventwatch_int_decr(eventwatch_client_t *c, const char *topic, int64_t delta) {
    cJSON *p = cJSON_CreateObject();
    cJSON_AddNumberToObject(p, "delta", (double)delta);
    publish_typed(c, topic, "int_decr", p);
}

void eventwatch_int_delete(eventwatch_client_t *c, const char *topic) {
    publish_typed(c, topic, "int_delete", NULL);
}

void eventwatch_time_now(eventwatch_client_t *c, const char *topic) {
    publish_typed(c, topic, "time_now", NULL);
}
