"""Async event_watch client.

One Client wraps one WebSocket. Subscribe returns a Handle so N callbacks on
the same topic share one upstream subscription — Handle.close() decrements
the local refcount and only sends unsubscribe upstream when the last handle
goes away. Reconnect is automatic; on resume the client re-subscribes each
topic starting at the seq after the last event it observed.
"""

from __future__ import annotations

import asyncio
import contextlib
import json
import logging
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any, Awaitable, Callable, Optional
from urllib.parse import urlencode, urlparse, urlunparse

import websockets

logger = logging.getLogger("eventwatch")

Callback = Callable[["Event"], Awaitable[None] | None]


@dataclass
class Event:
    id: str
    topic: str
    type: str
    seq: int
    occurred_at: str
    actor: str = ""
    payload: Optional[dict] = None
    state: Optional[dict] = None

    @staticmethod
    def from_dict(d: dict) -> "Event":
        return Event(
            id=d.get("id", ""),
            topic=d.get("topic", ""),
            type=d.get("type", ""),
            seq=int(d.get("seq", 0)),
            occurred_at=d.get("occurred_at", ""),
            actor=d.get("actor", ""),
            payload=d.get("payload"),
            state=d.get("state"),
        )


class Handle:
    def __init__(self, client: "Client", topic: str, cb_id: int):
        self._client = client
        self._topic = topic
        self._id = cb_id
        self._closed = False

    def close(self) -> None:
        if self._closed:
            return
        self._closed = True
        # Schedule the removal on the client's event loop so it's safe to
        # call from any thread / any coroutine.
        try:
            self._client._loop.call_soon_threadsafe(
                self._client._remove_callback, self._topic, self._id
            )
        except RuntimeError:
            # loop closed — client already dead, nothing to do
            pass


@dataclass
class _TopicSub:
    from_kind: str  # "latest" | "last:N" | "seq:N"
    callbacks: dict[int, Callback] = field(default_factory=dict)
    last_seen_seq: int = 0


class Client:
    """WebSocket client. Use `Client.dial(url, token=...)`."""

    def __init__(self, url: str, token: str = ""):
        self._url = url
        self._token = token
        self._loop = asyncio.get_event_loop()
        self._ws: Optional[websockets.WebSocketClientProtocol] = None
        self._topics: dict[str, _TopicSub] = {}
        self._pending: dict[str, asyncio.Future] = {}
        self._next_id = 0
        self._next_req = 0
        self._closed = False
        self._runner: Optional[asyncio.Task] = None
        self._sendq: asyncio.Queue = asyncio.Queue()
        self._connected = asyncio.Event()

    @classmethod
    async def dial(cls, url: str, *, token: str = "") -> "Client":
        c = cls(url, token=token)
        # eager connect so the caller learns immediately if URL is bad
        await c._connect_once()
        c._runner = asyncio.create_task(c._run())
        return c

    async def close(self) -> None:
        self._closed = True
        if self._ws:
            with contextlib.suppress(Exception):
                await self._ws.close()
        if self._runner:
            with contextlib.suppress(Exception):
                await self._runner

    # -- connection lifecycle --

    def _dial_url(self) -> str:
        if not self._token:
            return self._url
        parsed = urlparse(self._url)
        q = parsed.query
        extra = urlencode({"access_token": self._token})
        q = f"{q}&{extra}" if q else extra
        return urlunparse(parsed._replace(query=q))

    async def _connect_once(self) -> None:
        extra = {}
        if self._token:
            extra["Authorization"] = f"Bearer {self._token}"
        self._ws = await websockets.connect(self._dial_url(), additional_headers=extra)
        self._connected.set()

    async def _run(self) -> None:
        # After the eager connect, this loop owns reconnect + resume.
        backoff = 0.1
        while not self._closed:
            if self._ws is None:
                try:
                    await self._connect_once()
                    backoff = 0.1
                except Exception:
                    await asyncio.sleep(backoff)
                    backoff = min(backoff * 2, 30)
                    continue
            await self._resubscribe_all()
            reader = asyncio.create_task(self._reader())
            writer = asyncio.create_task(self._writer())
            done, pending = await asyncio.wait(
                {reader, writer}, return_when=asyncio.FIRST_COMPLETED
            )
            for t in pending:
                t.cancel()
                with contextlib.suppress(BaseException):
                    await t
            self._ws = None
            self._connected.clear()

    async def _reader(self) -> None:
        try:
            async for msg in self._ws:
                try:
                    frame = json.loads(msg)
                except Exception:
                    continue
                self._dispatch(frame)
        except Exception:
            return

    async def _writer(self) -> None:
        # Only exit on connection loss; the WS's own error will cascade to
        # the reader too, which will end _run's wait().
        try:
            while True:
                frame = await self._sendq.get()
                await self._ws.send(json.dumps(frame))
        except Exception:
            return

    def _dispatch(self, frame: dict) -> None:
        t = frame.get("type")
        if t == "event":
            ev_dict = frame.get("event")
            if not ev_dict:
                return
            ev = Event.from_dict(ev_dict)
            ts = self._topics.get(ev.topic)
            if not ts:
                return
            if ev.seq > ts.last_seen_seq:
                ts.last_seen_seq = ev.seq
            for cb in list(ts.callbacks.values()):
                res = cb(ev)
                if asyncio.iscoroutine(res):
                    asyncio.ensure_future(res)
        elif t in ("state", "ack", "error"):
            req_id = frame.get("req_id")
            if req_id and req_id in self._pending:
                fut = self._pending.pop(req_id)
                if t == "error":
                    fut.set_exception(RuntimeError(frame.get("message", "error")))
                else:
                    fut.set_result(frame)

    async def _resubscribe_all(self) -> None:
        for topic, ts in list(self._topics.items()):
            resume = ts.last_seen_seq + 1 if ts.last_seen_seq > 0 else 0
            self._send(_subscribe_frame(topic, ts.from_kind, resume))

    def _send(self, frame: dict) -> None:
        self._sendq.put_nowait(frame)

    async def _request(self, frame: dict) -> dict:
        self._next_req += 1
        req_id = f"r{self._next_req}"
        frame["req_id"] = req_id
        fut = self._loop.create_future()
        self._pending[req_id] = fut
        self._send(frame)
        return await fut

    # -- public API --

    async def subscribe(
        self, topic: str, callback: Callback, *, from_kind: str = "latest"
    ) -> Handle:
        """Register callback for topic. First subscriber for a topic opens the
        upstream sub; subsequent callbacks share it (refcounted)."""
        self._next_id += 1
        cb_id = self._next_id
        first = topic not in self._topics
        if first:
            self._topics[topic] = _TopicSub(from_kind=from_kind)
        self._topics[topic].callbacks[cb_id] = callback
        if first:
            self._send(_subscribe_frame(topic, from_kind, 0))
        return Handle(self, topic, cb_id)

    def _remove_callback(self, topic: str, cb_id: int) -> None:
        ts = self._topics.get(topic)
        if not ts:
            return
        ts.callbacks.pop(cb_id, None)
        if not ts.callbacks:
            del self._topics[topic]
            self._send({"op": "unsubscribe", "topic": topic})

    async def publish(
        self, topic: str, event_type: str, payload: Optional[dict] = None
    ) -> int:
        """Publish a single event; returns the assigned seq."""
        frame = {"op": "publish", "topic": topic, "type": event_type}
        if payload is not None:
            frame["payload"] = payload
        resp = await self._request(frame)
        return int(resp.get("last_seq", 0))

    async def get_state(self, topic: str) -> Optional[dict]:
        """Fetch the current computed state (None if none yet)."""
        try:
            resp = await self._request({"op": "get_state", "topic": topic})
        except RuntimeError as e:
            if "not found" in str(e).lower():
                return None
            raise
        state = resp.get("state")
        return state

    # -- typed field helpers --

    def string_field(self, topic: str) -> "StringField":
        return StringField(self, topic)

    def int_field(self, topic: str) -> "IntField":
        return IntField(self, topic)

    def time_field(self, topic: str) -> "TimeField":
        return TimeField(self, topic)


def _subscribe_frame(topic: str, from_kind: str, resume_seq: int) -> dict:
    f = {"op": "subscribe", "topic": topic}
    if resume_seq > 0:
        f["from_seq"] = resume_seq
    elif from_kind.startswith("last:") or from_kind.startswith("seq:"):
        f["from"] = from_kind
    else:
        f["from"] = "latest"
    return f


# -- typed field helpers --


class StringField:
    def __init__(self, client: Client, topic: str):
        self._c = client
        self.topic = topic

    async def set(self, value: str) -> int:
        return await self._c.publish(self.topic, "str_set", {"value": value})

    async def delete(self) -> int:
        return await self._c.publish(self.topic, "str_delete")

    async def get(self) -> tuple[str, bool]:
        s = await self._c.get_state(self.topic)
        if not s:
            return "", False
        return str(s.get("value", "")), bool(s.get("exists", False))


class IntField:
    def __init__(self, client: Client, topic: str):
        self._c = client
        self.topic = topic

    async def set(self, value: int) -> int:
        return await self._c.publish(self.topic, "int_set", {"value": value})

    async def incr(self, delta: int = 1) -> int:
        return await self._c.publish(self.topic, "int_incr", {"delta": delta})

    async def decr(self, delta: int = 1) -> int:
        return await self._c.publish(self.topic, "int_decr", {"delta": delta})

    async def delete(self) -> int:
        return await self._c.publish(self.topic, "int_delete")

    async def get(self) -> tuple[int, bool]:
        s = await self._c.get_state(self.topic)
        if not s:
            return 0, False
        return int(s.get("value", 0)), bool(s.get("exists", False))


class TimeField:
    def __init__(self, client: Client, topic: str):
        self._c = client
        self.topic = topic

    async def set(self, when: datetime) -> int:
        if when.tzinfo is None:
            when = when.replace(tzinfo=timezone.utc)
        else:
            when = when.astimezone(timezone.utc)
        # RFC3339 with nanosecond precision to match the Go server.
        s = when.isoformat().replace("+00:00", "Z")
        return await self._c.publish(self.topic, "time_set", {"value": s})

    async def now(self) -> int:
        return await self._c.publish(self.topic, "time_now")

    async def add(self, seconds: int) -> int:
        return await self._c.publish(self.topic, "time_add", {"seconds": seconds})

    async def delete(self) -> int:
        return await self._c.publish(self.topic, "time_delete")

    async def get(self) -> tuple[Optional[datetime], bool]:
        s = await self._c.get_state(self.topic)
        if not s:
            return None, False
        val = s.get("value")
        if not val:
            return None, bool(s.get("exists", False))
        # Server sends time with trailing 'Z' for UTC or offset like '-07:00'.
        # datetime.fromisoformat in 3.11 handles both.
        try:
            dt = datetime.fromisoformat(val.replace("Z", "+00:00"))
        except ValueError:
            return None, bool(s.get("exists", False))
        return dt, bool(s.get("exists", False))
