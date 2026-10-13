"""Every bridge HTTP call has a timeout, and bridge-calling tools stay off the event loop."""

import asyncio
import threading

import pytest
import requests

import main as mcp_main
import whatsapp


class _Resp:
    status_code = 200
    text = ""

    def __init__(self, payload=None):
        self._payload = payload or {"success": True, "ok": True, "message": "ok", "results": []}

    def json(self):
        return self._payload


@pytest.fixture
def seen(monkeypatch):
    """Record the timeout of every bridge call made through requests."""
    timeouts = []

    def record(url, *args, timeout=None, **kwargs):
        timeouts.append(timeout)
        return _Resp()

    monkeypatch.setattr(whatsapp.requests, "post", record)
    monkeypatch.setattr(whatsapp.requests, "get", record)
    monkeypatch.delenv("WHATSAPP_SEND_MIN_INTERVAL_MS", raising=False)
    return timeouts


def test_every_bridge_call_passes_a_timeout(seen, tmp_path):
    media = tmp_path / "a.jpg"
    media.write_bytes(b"x")
    audio = tmp_path / "a.ogg"
    audio.write_bytes(b"x")

    whatsapp.send_message("12025551234", "hi")
    whatsapp.send_file("12025551234", str(media))
    whatsapp.send_audio_message("12025551234", str(audio))
    whatsapp.send_reaction("12025551234@s.whatsapp.net", "ID", "👍")
    whatsapp.delete_message("12025551234@s.whatsapp.net", "ID")
    whatsapp.get_group_participants("1@g.us")
    whatsapp.mark_messages_read(["ID"], "12025551234@s.whatsapp.net")
    whatsapp.check_whatsapp(["12025551234"])
    whatsapp.download_media("ID", "12025551234@s.whatsapp.net")
    whatsapp.bridge_status()

    assert len(seen) == 10
    assert all(isinstance(t, int | float) and t > 0 for t in seen)


def test_send_timeout_outlasts_the_bridge_pacing_interval(seen, monkeypatch):
    monkeypatch.setenv("WHATSAPP_SEND_MIN_INTERVAL_MS", "45000")

    whatsapp.send_message("12025551234", "hi")

    assert seen[0] == 45 + whatsapp.SEND_MARGIN_S


@pytest.mark.parametrize("value", ["", "abc", "-5"])
def test_unusable_pacing_value_falls_back_to_margin(seen, monkeypatch, value):
    monkeypatch.setenv("WHATSAPP_SEND_MIN_INTERVAL_MS", value)

    whatsapp.send_message("12025551234", "hi")

    assert seen[0] == whatsapp.SEND_MARGIN_S


def test_read_timeout_on_send_warns_it_may_have_been_delivered(monkeypatch):
    def slow(url, *args, **kwargs):
        raise requests.ReadTimeout("read timed out")

    monkeypatch.setattr(whatsapp.requests, "post", slow)

    success, message = whatsapp.send_message("12025551234", "hi")

    assert success is False
    assert "may still have been delivered" in message


def test_bridge_calling_tools_run_off_the_event_loop(monkeypatch):
    loop_thread = threading.get_ident()
    ran_in = []

    def fake_check(numbers):
        ran_in.append(threading.get_ident())
        return {"success": True, "results": []}

    monkeypatch.setattr(mcp_main, "whatsapp_check_whatsapp", fake_check)

    asyncio.run(mcp_main.mcp.call_tool("check_whatsapp", {"phone_numbers": ["12025551234"]}))

    assert ran_in and ran_in[0] != loop_thread


def test_blocking_tools_keep_their_schema():
    tools = {tool.name: tool for tool in asyncio.run(mcp_main.mcp.list_tools())}

    assert tools["view_media"].inputSchema["properties"]["max_dimension"]["maximum"] == 2048
    assert tools["send_message"].description.startswith("Send a WhatsApp message")
    assert set(tools["send_message"].inputSchema["properties"]) >= {"recipient", "message", "mentions"}
