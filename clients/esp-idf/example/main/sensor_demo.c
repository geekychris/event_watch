// Minimal example: connect to WiFi, then to the event_watch server, and
// every second increment `int/esp32/uptime_seconds` by 1. Also subscribes
// to `str/esp32/cmd` and prints any command posted to it.
//
// Set your WiFi + server details in menuconfig ("Example Config") or by
// editing the CONFIG_* defaults below.
#include <string.h>
#include <stdio.h>

#include "freertos/FreeRTOS.h"
#include "freertos/task.h"
#include "freertos/event_groups.h"

#include "esp_wifi.h"
#include "esp_event.h"
#include "esp_log.h"
#include "nvs_flash.h"

#include "eventwatch.h"

static const char *TAG = "sensor_demo";

// Adjust these for your setup, or wire up sdkconfig entries.
#ifndef WIFI_SSID
#define WIFI_SSID "your-ssid"
#endif
#ifndef WIFI_PASS
#define WIFI_PASS "your-pass"
#endif
#ifndef EW_URL
#define EW_URL "ws://192.168.1.10:8080/ws"
#endif

static EventGroupHandle_t s_wifi_group;
#define WIFI_CONNECTED_BIT BIT0

static void wifi_event_handler(void *arg, esp_event_base_t base, int32_t id, void *data) {
    if (base == WIFI_EVENT && id == WIFI_EVENT_STA_START) {
        esp_wifi_connect();
    } else if (base == WIFI_EVENT && id == WIFI_EVENT_STA_DISCONNECTED) {
        esp_wifi_connect();
        xEventGroupClearBits(s_wifi_group, WIFI_CONNECTED_BIT);
    } else if (base == IP_EVENT && id == IP_EVENT_STA_GOT_IP) {
        xEventGroupSetBits(s_wifi_group, WIFI_CONNECTED_BIT);
    }
}

static void wifi_init(void) {
    s_wifi_group = xEventGroupCreate();
    ESP_ERROR_CHECK(esp_netif_init());
    ESP_ERROR_CHECK(esp_event_loop_create_default());
    esp_netif_create_default_wifi_sta();

    wifi_init_config_t cfg = WIFI_INIT_CONFIG_DEFAULT();
    ESP_ERROR_CHECK(esp_wifi_init(&cfg));

    ESP_ERROR_CHECK(esp_event_handler_instance_register(WIFI_EVENT, ESP_EVENT_ANY_ID,
                                                        wifi_event_handler, NULL, NULL));
    ESP_ERROR_CHECK(esp_event_handler_instance_register(IP_EVENT, IP_EVENT_STA_GOT_IP,
                                                        wifi_event_handler, NULL, NULL));

    wifi_config_t wc = {0};
    strncpy((char *)wc.sta.ssid, WIFI_SSID, sizeof(wc.sta.ssid) - 1);
    strncpy((char *)wc.sta.password, WIFI_PASS, sizeof(wc.sta.password) - 1);
    ESP_ERROR_CHECK(esp_wifi_set_mode(WIFI_MODE_STA));
    ESP_ERROR_CHECK(esp_wifi_set_config(WIFI_IF_STA, &wc));
    ESP_ERROR_CHECK(esp_wifi_start());

    ESP_LOGI(TAG, "connecting to WiFi...");
    xEventGroupWaitBits(s_wifi_group, WIFI_CONNECTED_BIT, pdFALSE, pdTRUE, portMAX_DELAY);
    ESP_LOGI(TAG, "wifi connected");
}

static void on_event(const char *topic, const char *type, uint64_t seq,
                     const char *payload_json, const char *state_json, void *user) {
    ESP_LOGI(TAG, "sub: topic=%s type=%s seq=%llu payload=%s state=%s",
             topic, type, (unsigned long long)seq, payload_json, state_json);
}

void app_main(void) {
    ESP_ERROR_CHECK(nvs_flash_init());
    wifi_init();

    eventwatch_config_t cfg = {
        .url = EW_URL,
        .event_cb = on_event,
    };
    eventwatch_client_t *c = eventwatch_start(&cfg);

    // Subscribe to a "command" channel.
    eventwatch_subscribe(c, "str/esp32/cmd", "latest");

    // Publish loop: bump uptime every second.
    while (1) {
        eventwatch_int_incr(c, "int/esp32/uptime_seconds", 1);
        vTaskDelay(pdMS_TO_TICKS(1000));
    }
}
