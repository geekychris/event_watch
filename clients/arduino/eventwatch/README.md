# EventWatch — Arduino library

Client library for the event_watch pub/sub service, targeting ESP32 and
ESP8266 via the Arduino framework.

## Install

Copy `eventwatch/` into your `Arduino/libraries/` directory (or symlink it),
then in the Library Manager install:

- **WebSockets** by Markus Sattler (Links2004/arduinoWebSockets)
- **ArduinoJson** by Benoit Blanchon (v6+)

Then `#include <EventWatch.h>` in your sketch.

## Use

See `examples/IntCounter/IntCounter.ino` for a full sketch. Minimal skeleton:

```cpp
#include <WiFi.h>
#include <EventWatch.h>

EventWatch ew;

void onEvent(const char *topic, const char *type, uint64_t seq,
             JsonVariant payload, JsonVariant state) {
    Serial.print(topic); Serial.print(" "); Serial.println(type);
}

void setup() {
    WiFi.begin("ssid", "pass");
    while (WiFi.status() != WL_CONNECTED) delay(300);

    ew.onEvent(onEvent);
    ew.begin("192.168.1.10", 8080, "/ws");
    ew.subscribe("int/esp32/uptime_seconds", "latest");
}

void loop() {
    ew.loop();
    static unsigned long last = 0;
    if (millis() - last > 1000) {
        last = millis();
        ew.intIncr("int/esp32/uptime_seconds", 1);
    }
}
```

## Field helpers

```cpp
ew.strSet("str/status", "ok");
ew.strDelete("str/status");
ew.intSet("int/counter", 100);
ew.intIncr("int/counter", 5);
ew.intDecr("int/counter", 3);
ew.intDelete("int/counter");
ew.timeNow("time/last_seen");
```

## Kept lean

- Up to 8 concurrent topics (bump `kMaxTopics` in `EventWatch.h` if you need more)
- No refcounted local dispatch — one callback for all subscribed topics
- No request/response over WS — read current values via the `state` param
  delivered inline on each subscribed event
- Auto-reconnect with `setReconnectInterval(3000)`; re-issues subscribes with
  `from_seq=lastSeq+1` on reconnect so no events are missed unless the server's
  archiver has already trimmed the topic
