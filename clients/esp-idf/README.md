# eventwatch — ESP-IDF component

Native ESP-IDF component for event_watch. Uses `esp_websocket_client` +
`cJSON`. Suitable for ESP32 / ESP32-S3 / ESP32-C3 (anything ESP-IDF supports).

## Install

Drop `eventwatch/` next to your project's other components (or set
`EXTRA_COMPONENT_DIRS` in your top-level CMakeLists.txt).

The component declares its WS-client dependency in `idf_component.yml`; on
`idf.py build` the IDF component manager will pull `esp_websocket_client`
into `managed_components/` automatically.

## Use

```c
#include "eventwatch.h"

static void on_event(const char *topic, const char *type, uint64_t seq,
                     const char *payload_json, const char *state_json, void *user) {
    printf("%s %s seq=%llu state=%s\n", topic, type,
           (unsigned long long)seq, state_json);
}

void app_main(void) {
    eventwatch_config_t cfg = {
        .url      = "ws://192.168.1.10:8080/ws",
        .event_cb = on_event,
    };
    eventwatch_client_t *c = eventwatch_start(&cfg);
    eventwatch_subscribe(c, "int/esp32/uptime_seconds", "latest");
    while (1) {
        eventwatch_int_incr(c, "int/esp32/uptime_seconds", 1);
        vTaskDelay(pdMS_TO_TICKS(1000));
    }
}
```

## Example

`example/` contains a full sensor-loop demo (`sensor_demo.c`) that connects
WiFi then increments an uptime counter every second and subscribes to a
command topic. To build:

```bash
cd clients/esp-idf/example
idf.py set-target esp32
idf.py menuconfig    # set WiFi SSID/pass + EW_URL, or edit sensor_demo.c
idf.py build
idf.py -p /dev/tty.usbserial-XXXX flash monitor
```

## Toolchain note

ESP-IDF v5.4 is not compatible with CMake 4.x — the `define_property command is not scriptable`
error means your Homebrew CMake is too new. Either upgrade to IDF v5.5+ or install
an older CMake (`brew install cmake@3.28` and prepend it to `PATH` before sourcing
`export.sh`, or use `idf_tools.py install cmake` if your cert store lets you).

## Not implemented (kept lean for MCU)

- Refcounted local dispatch — one client, one callback for all subscribed topics
- Request/response over WS (no `get_state`) — use `state_json` delivered inline with events
- More than 16 concurrent topics (increase `EW_MAX_TOPICS` in `eventwatch.c` if you need more)
