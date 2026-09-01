// EventWatch — Arduino client for the event_watch pub/sub service.
//
// Depends on:
//   - Links2004's WebSockets (arduinoWebSockets) — https://github.com/Links2004/arduinoWebSockets
//   - Bodmer's ArduinoJson v6+
//
// Designed for ESP32/ESP8266 (needs WiFi). Small surface: publish + subscribe
// + int/str field helpers. No refcounted dispatch, no request/response, no
// resume-seq on reconnect. Auto-reconnects and re-issues subscribes.
#ifndef EVENTWATCH_H
#define EVENTWATCH_H

#include <Arduino.h>
#include <ArduinoJson.h>
#include <WebSocketsClient.h>
#include <functional>

class EventWatch {
public:
    // Called for each event on a subscribed topic. `state` may be a null
    // JsonVariant if the event's topic has no reducer. Copy anything you
    // need out of `payload`/`state` before returning — both are backed by
    // buffers that will be reused for the next frame.
    using EventCallback = std::function<void(const char *topic, const char *type,
                                              uint64_t seq, JsonVariant payload,
                                              JsonVariant state)>;

    EventWatch();

    // Connect to ws://host:port/ws (or wss with .beginSSL / setUseSSL).
    // `authToken` may be empty for no-auth servers.
    void begin(const char *host, uint16_t port, const char *path = "/ws",
               const char *authToken = "");
    void beginSSL(const char *host, uint16_t port, const char *path = "/ws",
                  const char *authToken = "");

    // Call this from loop().
    void loop();

    void onEvent(EventCallback cb) { _cb = cb; }

    bool connected() const { return _connected; }

    // Subscribe / unsubscribe. `from` = "latest" or "last:N" or "seq:N".
    void subscribe(const char *topic, const char *from = "latest");
    void unsubscribe(const char *topic);

    // Raw publish (payload_json may be nullptr).
    void publishJson(const char *topic, const char *type, const char *payload_json);

    // Field helpers.
    void strSet(const char *topic, const char *value);
    void strDelete(const char *topic);
    void intSet(const char *topic, int64_t value);
    void intIncr(const char *topic, int64_t delta = 1);
    void intDecr(const char *topic, int64_t delta = 1);
    void intDelete(const char *topic);
    void timeNow(const char *topic);

private:
    static const int kMaxTopics = 8;
    struct Sub { String topic; String from; uint64_t lastSeq; };

    WebSocketsClient _ws;
    EventCallback _cb;
    Sub _subs[kMaxTopics];
    int _subCount = 0;
    String _authToken;
    bool _connected = false;
    bool _useSSL = false;

    static EventWatch *_singleton;   // WebSocketsClient callback needs static

    void _onWSEvent(WStype_t type, uint8_t *payload, size_t length);
    void _resubscribeAll();
    int  _findSub(const char *topic);

    void _sendFrame(JsonDocument &doc);
    void _publishTyped(const char *topic, const char *type, JsonVariant payloadVar);
};

#endif  // EVENTWATCH_H
