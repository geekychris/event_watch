# eventwatch-client — Java

Java 11+ client for event_watch. Uses the built-in `java.net.http.WebSocket`;
only third-party dep is Jackson for JSON.

## Build

```bash
cd clients/java
mvn -q package
```

## Use

```java
try (Client c = Client.dial("ws://localhost:8080/ws")) {
    IntField counter = c.intField("int/counter");
    counter.set(100);
    counter.incr(5);         // → 105
    Object[] v = counter.get();  // { Long, Boolean }
    System.out.println(v[0] + " exists=" + v[1]);

    Handle h = c.subscribe("int/counter", ev ->
        System.out.println(ev.type + " seq=" + ev.seq + " state=" + ev.state));
    counter.incr(1);
    Thread.sleep(200);
    h.close();
}
```

## Test

Requires a running event_watch server on `:8080`. Integration tests skip
silently if not reachable.

```bash
cd clients/java
mvn -q test
```
