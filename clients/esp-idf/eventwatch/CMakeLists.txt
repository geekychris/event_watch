idf_component_register(
    SRCS "eventwatch.c"
    INCLUDE_DIRS "include"
    REQUIRES esp_websocket_client json esp_event
)
