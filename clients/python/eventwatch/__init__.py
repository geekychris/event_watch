"""Python client for the event_watch pub/sub service.

Basic usage:

    import asyncio
    from eventwatch import Client

    async def main():
        c = await Client.dial("ws://localhost:8080/ws")
        # subscribe
        async def on_event(ev):
            print(ev.type, ev.seq, ev.state)
        handle = await c.subscribe("int/counter", on_event)
        # publish + arithmetic
        counter = c.int_field("int/counter")
        await counter.set(100)
        await counter.incr(5)
        v, exists = await counter.get()
        print("value:", v)
        handle.close()
        await c.close()

    asyncio.run(main())
"""

from .client import Client, Event, Handle, StringField, IntField, TimeField

__all__ = ["Client", "Event", "Handle", "StringField", "IntField", "TimeField"]
