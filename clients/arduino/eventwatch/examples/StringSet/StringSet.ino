// StringSet — connect to WiFi + event_watch, set `str/esp32/status` on boot
// and update it whenever the loop sensor reading crosses a threshold.

#include <WiFi.h>
#include <EventWatch.h>

const char *WIFI_SSID = "your-ssid";
const char *WIFI_PASS = "your-pass";
const char *EW_HOST   = "192.168.1.10";
const uint16_t EW_PORT = 8080;

EventWatch ew;
bool wasHot = false;

void setup() {
    Serial.begin(115200);
    WiFi.begin(WIFI_SSID, WIFI_PASS);
    while (WiFi.status() != WL_CONNECTED) delay(300);
    ew.begin(EW_HOST, EW_PORT, "/ws");
    while (!ew.connected()) { ew.loop(); delay(50); }
    ew.strSet("str/esp32/status", "booted");
}

void loop() {
    ew.loop();
    int temp = analogRead(A0);            // arbitrary "sensor"
    bool hot = temp > 2048;
    if (hot != wasHot) {
        ew.strSet("str/esp32/status", hot ? "hot" : "cool");
        wasHot = hot;
    }
    delay(500);
}
