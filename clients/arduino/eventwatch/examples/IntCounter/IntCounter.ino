// IntCounter — connect to WiFi + event_watch, increment `int/esp32/uptime_seconds`
// once a second, and subscribe to `str/esp32/cmd` for remote commands.

#include <WiFi.h>          // ESP32; for ESP8266 use <ESP8266WiFi.h>
#include <EventWatch.h>

const char *WIFI_SSID = "your-ssid";
const char *WIFI_PASS = "your-pass";
const char *EW_HOST   = "192.168.1.10";
const uint16_t EW_PORT = 8080;

EventWatch ew;

void onEvent(const char *topic, const char *type, uint64_t seq,
             JsonVariant payload, JsonVariant state) {
    Serial.print("[event] "); Serial.print(topic);
    Serial.print(" "); Serial.print(type);
    Serial.print(" seq="); Serial.println((unsigned long)seq);
    if (!state.isNull()) {
        Serial.print("  state: ");
        serializeJson(state, Serial);
        Serial.println();
    }
}

void setup() {
    Serial.begin(115200);
    delay(200);

    WiFi.begin(WIFI_SSID, WIFI_PASS);
    Serial.print("wifi");
    while (WiFi.status() != WL_CONNECTED) { delay(300); Serial.print("."); }
    Serial.println();
    Serial.print("ip "); Serial.println(WiFi.localIP());

    ew.onEvent(onEvent);
    ew.begin(EW_HOST, EW_PORT, "/ws");
    ew.subscribe("str/esp32/cmd", "latest");
}

unsigned long lastTick = 0;
void loop() {
    ew.loop();
    unsigned long now = millis();
    if (now - lastTick > 1000) {
        lastTick = now;
        if (ew.connected()) ew.intIncr("int/esp32/uptime_seconds", 1);
    }
}
