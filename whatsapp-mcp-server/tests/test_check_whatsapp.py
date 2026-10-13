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


def test_check_whatsapp_posts_numbers_and_returns_results(monkeypatch):
    calls = []
    results = [{"input": "+1 647-669-8464", "jid": "123@lid", "registered": True}]

    def fake_post(url, json, headers=None, timeout=None):
        calls.append({"url": url, "json": json, "headers": headers})
        return _Resp(payload={"results": results})

    monkeypatch.setenv("WHATSAPP_BRIDGE_TOKEN", "env-token")
    monkeypatch.setattr(whatsapp.requests, "post", fake_post)

    out = mcp_main.check_whatsapp(["+1 647-669-8464"])

    assert out == {"success": True, "results": results}
    assert calls[0]["url"].endswith("/check")
    assert calls[0]["json"] == {"phone_numbers": ["+1 647-669-8464"]}
    assert calls[0]["headers"] == {"Authorization": "Bearer env-token"}


def test_check_whatsapp_empty_list_does_not_call_bridge(monkeypatch):
    monkeypatch.setattr(whatsapp.requests, "post", lambda *a, **k: pytest.fail("bridge called"))

    out = whatsapp.check_whatsapp([])

    assert out["success"] is False


def test_check_whatsapp_bridge_error_is_surfaced(monkeypatch):
    monkeypatch.setattr(
        whatsapp.requests,
        "post",
        lambda *a, **k: _Resp(status_code=503, text='{"success":false}'),
    )

    out = whatsapp.check_whatsapp(["16476698464"])

    assert out["success"] is False
    assert "HTTP 503" in out["message"]


def test_check_whatsapp_reports_unreachable_bridge(monkeypatch):
    def refuse(*a, **k):
        raise requests.ConnectionError("Connection refused")

    monkeypatch.setattr(whatsapp.requests, "post", refuse)

    out = whatsapp.check_whatsapp(["16476698464"])

    assert out["success"] is False
    assert out["message"].startswith(f"Bridge not reachable at {whatsapp.WHATSAPP_API_BASE_URL}")
