"""Integration tests. Require a running event_watch server on :8080.

Run with:  pytest -q  from clients/python/
"""

import asyncio
import os

import pytest

from eventwatch import Client

SERVER = os.environ.get("EW_URL", "ws://localhost:8080/ws")


async def _client():
    return await Client.dial(SERVER)


async def test_int_field_set_incr_decr_get():
    c = await _client()
    try:
        f = c.int_field("int/pytest/counter")
        await f.set(100)
        await f.incr(5)
        await f.incr()  # default +1
        await f.decr(4)
        v, ok = await f.get()
        assert ok is True
        assert v == 102
    finally:
        await c.close()


async def test_string_field_set_delete_get():
    c = await _client()
    try:
        f = c.string_field("str/pytest/name")
        await f.set("alice")
        v, ok = await f.get()
        assert v == "alice" and ok
        await f.delete()
        v, ok = await f.get()
        assert v == "" and not ok
    finally:
        await c.close()


async def test_subscribe_receives_events_with_state():
    c = await _client()
    try:
        events = []
        got = asyncio.Event()

        async def cb(ev):
            events.append(ev)
            if len(events) >= 3:
                got.set()

        h = await c.subscribe("int/pytest/live", cb)
        try:
            # Small settle for subscribe to reach the server.
            await asyncio.sleep(0.1)
            f = c.int_field("int/pytest/live")
            await f.set(10)
            await f.incr(5)
            await f.decr(2)
            await asyncio.wait_for(got.wait(), timeout=3)
            # State should be attached on the fanned-out frames.
            assert events[-1].state is not None
            assert events[-1].state.get("value") == 13
        finally:
            h.close()
    finally:
        await c.close()


async def test_refcounted_subscribe():
    c = await _client()
    try:
        a_ct = 0
        b_ct = 0
        done_a = asyncio.Event()
        done_b = asyncio.Event()

        def cb_a(_ev):
            nonlocal a_ct
            a_ct += 1
            done_a.set()

        def cb_b(_ev):
            nonlocal b_ct
            b_ct += 1
            done_b.set()

        h1 = await c.subscribe("int/pytest/refcount", cb_a)
        h2 = await c.subscribe("int/pytest/refcount", cb_b)
        try:
            await asyncio.sleep(0.1)
            await c.int_field("int/pytest/refcount").set(1)
            await asyncio.wait_for(done_a.wait(), timeout=2)
            await asyncio.wait_for(done_b.wait(), timeout=2)
            assert a_ct == 1 and b_ct == 1
        finally:
            h1.close()
            h2.close()
    finally:
        await c.close()
