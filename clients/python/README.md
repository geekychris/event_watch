# eventwatch — Python client

Async client for the event_watch pub/sub service.

## Install

```bash
pip install -e clients/python
# or
pip install websockets  # only runtime dep, then just add the eventwatch/ dir to your PYTHONPATH
```

## Quick start

```python
import asyncio
from eventwatch import Client

async def main():
    c = await Client.dial("ws://localhost:8080/ws")

    # int field arithmetic
    counter = c.int_field("int/counter")
    await counter.set(100)
    await counter.incr(5)          # → 105
    await counter.decr(3)          # → 102
    value, exists = await counter.get()
    print(value, exists)           # 102 True

    # subscribe (receives events with `.state` attached)
    async def on_event(ev):
        print(ev.type, ev.seq, ev.state)
    h = await c.subscribe("int/counter", on_event)

    await counter.incr(1)          # subscriber sees int_incr with state.value=103
    await asyncio.sleep(0.1)

    h.close()
    await c.close()

asyncio.run(main())
```

## Test

With a running server on `:8080`:

```bash
cd clients/python
pip install -e '.[test]'
pytest -q
```
