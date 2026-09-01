#include "EventWatch.h"

EventWatch *EventWatch::_singleton = nullptr;

EventWatch::EventWatch() {
    _singleton = this;
}

void EventWatch::begin(const char *host, uint16_t port, const char *path,
                       const char *authToken) {
    _authToken = authToken ? authToken : "";
    // If a token is set, append it as ?access_token=... to the WS path — the
    // Arduino WS client can't set arbitrary headers pre-handshake reliably.
    String p = path ? String(path) : String("/ws");
    if (_authToken.length() > 0) {
        p += p.indexOf('?') >= 0 ? '&' : '?';
        p += "access_token=";
        p += _authToken;
    }
    _useSSL = false;
    _ws.begin(host, port, p.c_str());
    _ws.onEvent([](WStype_t type, uint8_t *payload, size_t length) {
        if (_singleton) _singleton->_onWSEvent(type, payload, length);
    });
    _ws.setReconnectInterval(3000);
}

void EventWatch::beginSSL(const char *host, uint16_t port, const char *path,
                          const char *authToken) {
    _authToken = authToken ? authToken : "";
    String p = path ? String(path) : String("/ws");
    if (_authToken.length() > 0) {
        p += p.indexOf('?') >= 0 ? '&' : '?';
        p += "access_token=";
        p += _authToken;
    }
    _useSSL = true;
    _ws.beginSSL(host, port, p.c_str());
    _ws.onEvent([](WStype_t type, uint8_t *payload, size_t length) {
        if (_singleton) _singleton->_onWSEvent(type, payload, length);
    });
    _ws.setReconnectInterval(3000);
}

void EventWatch::loop() { _ws.loop(); }

void EventWatch::_onWSEvent(WStype_t type, uint8_t *payload, size_t length) {
    switch (type) {
    case WStype_CONNECTED:
        _connected = true;
        _resubscribeAll();
        break;
    case WStype_DISCONNECTED:
        _connected = false;
        break;
    case WStype_TEXT: {
        StaticJsonDocument<2048> doc;
        DeserializationError err = deserializeJson(doc, payload, length);
        if (err) break;
        const char *ft = doc["type"];
        if (!ft || strcmp(ft, "event") != 0) break;
        JsonObject ev = doc["event"].as<JsonObject>();
        if (ev.isNull()) break;
        const char *topic = ev["topic"];
        const char *etype = ev["type"];
        uint64_t seq = ev["seq"].as<uint64_t>();
        JsonVariant payloadV = ev["payload"];
        JsonVariant stateV   = ev["state"];
        if (topic) {
            int idx = _findSub(topic);
            if (idx >= 0 && seq > _subs[idx].lastSeq) _subs[idx].lastSeq = seq;
            if (_cb) _cb(topic, etype ? etype : "", seq, payloadV, stateV);
        }
        break;
    }
    default: break;
    }
}

int EventWatch::_findSub(const char *topic) {
    for (int i = 0; i < _subCount; i++) {
        if (_subs[i].topic.equals(topic)) return i;
    }
    return -1;
}

void EventWatch::_resubscribeAll() {
    for (int i = 0; i < _subCount; i++) {
        StaticJsonDocument<256> d;
        d["op"] = "subscribe";
        d["topic"] = _subs[i].topic;
        if (_subs[i].lastSeq > 0) d["from_seq"] = _subs[i].lastSeq + 1;
        else d["from"] = _subs[i].from;
        _sendFrame(d);
    }
}

void EventWatch::subscribe(const char *topic, const char *from) {
    if (!topic) return;
    if (_findSub(topic) < 0 && _subCount < kMaxTopics) {
        _subs[_subCount].topic   = topic;
        _subs[_subCount].from    = from && *from ? from : "latest";
        _subs[_subCount].lastSeq = 0;
        _subCount++;
    }
    StaticJsonDocument<256> d;
    d["op"] = "subscribe";
    d["topic"] = topic;
    d["from"]  = from && *from ? from : "latest";
    _sendFrame(d);
}

void EventWatch::unsubscribe(const char *topic) {
    if (!topic) return;
    int idx = _findSub(topic);
    if (idx >= 0) {
        for (int i = idx; i < _subCount - 1; i++) _subs[i] = _subs[i + 1];
        _subCount--;
    }
    StaticJsonDocument<128> d;
    d["op"] = "unsubscribe";
    d["topic"] = topic;
    _sendFrame(d);
}

void EventWatch::publishJson(const char *topic, const char *type, const char *payload_json) {
    if (!topic || !type) return;
    StaticJsonDocument<1024> d;
    d["op"]    = "publish";
    d["topic"] = topic;
    d["type"]  = type;
    if (payload_json && *payload_json) {
        StaticJsonDocument<512> p;
        if (deserializeJson(p, payload_json) == DeserializationError::Ok) {
            d["payload"] = p;
        }
    }
    _sendFrame(d);
}

void EventWatch::_publishTyped(const char *topic, const char *type, JsonVariant payloadVar) {
    StaticJsonDocument<256> d;
    d["op"]    = "publish";
    d["topic"] = topic;
    d["type"]  = type;
    if (!payloadVar.isNull()) d["payload"] = payloadVar;
    _sendFrame(d);
}

void EventWatch::_sendFrame(JsonDocument &doc) {
    String out;
    serializeJson(doc, out);
    _ws.sendTXT(out);
}

void EventWatch::strSet(const char *topic, const char *value) {
    StaticJsonDocument<128> p;
    p["value"] = value ? value : "";
    _publishTyped(topic, "str_set", p.as<JsonVariant>());
}
void EventWatch::strDelete(const char *topic) { _publishTyped(topic, "str_delete", JsonVariant()); }

void EventWatch::intSet(const char *topic, int64_t value) {
    StaticJsonDocument<64> p; p["value"] = value; _publishTyped(topic, "int_set", p.as<JsonVariant>());
}
void EventWatch::intIncr(const char *topic, int64_t delta) {
    StaticJsonDocument<64> p; p["delta"] = delta; _publishTyped(topic, "int_incr", p.as<JsonVariant>());
}
void EventWatch::intDecr(const char *topic, int64_t delta) {
    StaticJsonDocument<64> p; p["delta"] = delta; _publishTyped(topic, "int_decr", p.as<JsonVariant>());
}
void EventWatch::intDelete(const char *topic) { _publishTyped(topic, "int_delete", JsonVariant()); }
void EventWatch::timeNow(const char *topic)   { _publishTyped(topic, "time_now", JsonVariant()); }
