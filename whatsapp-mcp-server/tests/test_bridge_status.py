import sqlite3

import pytest
import requests

import main as mcp_main
import whatsapp


class _Resp:
    def __init__(self, status_code=200, payload=None, text=""):
        self.status_code = status_code
        self._payload = payload if payload is not None else {}
        self.text = text

    def json(self):
        return self._payload


@pytest.fixture
def messages_db(tmp_path, monkeypatch):
    path = tmp_path / "messages.db"
    conn = sqlite3.connect(path)
    conn.execute("CREATE TABLE messages (id TEXT, timestamp TIMESTAMP)")
    conn.executemany(
        "INSERT INTO messages VALUES (?, ?)",
        [("a", "2026-10-01 08:00:00+00:00"), ("b", "2026-10-02 09:30:00+00:00")],
    )
    conn.commit()
    conn.close()
    monkeypatch.setattr(whatsapp, "MESSAGES_DB_PATH", str(path))
    return path


def test_bridge_status_reports_connected_bridge_and_newest_message(monkeypatch, messages_db):
    calls = []

    def fake_get(url, headers=None, timeout=None):
        calls.append({"url": url, "headers": headers})
        return _Resp(payload={"status": "ok", "connected": True, "timestamp": 1})

    monkeypatch.setenv("WHATSAPP_BRIDGE_TOKEN", "env-token")
    monkeypatch.setattr(whatsapp.requests, "get", fake_get)

    out = mcp_main.bridge_status()

    assert calls[0]["url"].endswith("/health")
    assert calls[0]["headers"] == {"Authorization": "Bearer env-token"}
    assert out["reachable"] is True
    assert out["connected"] is True
    assert out["status"] == "ok"
    assert out["db_path"] == str(messages_db)
    assert out["newest_message_timestamp"] == "2026-10-02T09:30:00+00:00"
    assert "db_error" not in out


def test_bridge_status_reports_disconnected_bridge(monkeypatch, messages_db):
    monkeypatch.setattr(
        whatsapp.requests,
        "get",
        lambda *a, **k: _Resp(503, {"status": "disconnected", "connected": False}),
    )

    out = whatsapp.bridge_status()

    assert out["reachable"] is True
    assert out["connected"] is False
    assert out["status"] == "disconnected"


def test_bridge_status_reports_dead_bridge_but_still_reads_db(monkeypatch, messages_db):
    def refuse(*a, **k):
        raise requests.ConnectionError("Connection refused")

    monkeypatch.setattr(whatsapp.requests, "get", refuse)

    out = whatsapp.bridge_status()

    assert out["reachable"] is False
    assert out["connected"] is None
    assert out["status"] == "unreachable"
    assert out["message"].startswith(f"Bridge not reachable at {whatsapp.WHATSAPP_API_BASE_URL}")
    assert out["newest_message_timestamp"] == "2026-10-02T09:30:00+00:00"


def test_bridge_status_missing_db_reports_error_without_creating_it(monkeypatch, tmp_path):
    missing = tmp_path / "nope" / "messages.db"
    monkeypatch.setattr(whatsapp, "MESSAGES_DB_PATH", str(missing))
    monkeypatch.setattr(whatsapp.requests, "get", lambda *a, **k: _Resp(payload={"status": "ok", "connected": True}))

    out = whatsapp.bridge_status()

    assert out["newest_message_timestamp"] is None
    assert "db_error" in out
    assert not missing.exists()


@pytest.mark.parametrize(
    ("func_name", "args"),
    [
        ("send_message", ("12025551234", "hello")),
        ("send_file", ("12025551234", "FILE")),
        ("send_audio_message", ("12025551234", "FILE")),
        ("send_reaction", ("12025551234@s.whatsapp.net", "3AABCDEF01234567", "👍")),
        ("mark_messages_read", (["3AABCDEF01234567"], "12025551234@s.whatsapp.net")),
    ],
)
def test_send_tools_report_unreachable_bridge(monkeypatch, tmp_path, func_name, args):
    media_file = tmp_path / "voice.ogg"
    media_file.write_bytes(b"ogg")
    resolved_args = tuple(str(media_file) if arg == "FILE" else arg for arg in args)

    def refuse(*a, **k):
        raise requests.ConnectionError("Max retries exceeded ... Connection refused")

    monkeypatch.setattr(whatsapp.requests, "post", refuse)

    success, message = getattr(whatsapp, func_name)(*resolved_args)

    assert success is False
    assert message == whatsapp._bridge_unreachable_message()
    assert "Max retries" not in message


def test_send_message_surfaces_bridge_error_code_and_message(monkeypatch):
    body = {"success": False, "error": "not_on_whatsapp", "message": "16476698464 is not registered on WhatsApp"}
    monkeypatch.setattr(whatsapp.requests, "post", lambda *a, **k: _Resp(422, body, text=str(body)))

    success, message = whatsapp.send_message("16476698464", "hi")

    assert success is False
    assert message == "Error: HTTP 422 not_on_whatsapp - 16476698464 is not registered on WhatsApp"
