"""Tests for the optional bearer-token auth in front of the http/sse transports."""

import pytest
from mcp.server.fastmcp import FastMCP
from starlette.testclient import TestClient

from mcp_auth import BearerTokenMiddleware
from mcp_config import require_auth_off_loopback, resolve_auth_token

TOKEN = "s3cret-token"


def _app(kind: str):
    server = FastMCP("test")
    app = server.sse_app() if kind == "sse" else server.streamable_http_app()
    app.add_middleware(BearerTokenMiddleware, token=TOKEN)
    # POST never opens an event stream, so accepted requests return immediately
    # (with a 4xx from the MCP handler, which is all these tests need to see).
    return app, ("/messages/" if kind == "sse" else "/mcp")


def _status(kind: str, authorization: str | None) -> int:
    app, path = _app(kind)
    headers = {"Host": "localhost:8000", "Accept": "application/json, text/event-stream"}
    if authorization is not None:
        headers["Authorization"] = authorization
    with TestClient(app) as client:
        return client.post(path, headers=headers, json={}).status_code


class TestResolveAuthToken:
    @pytest.mark.parametrize("value", [None, "", "   "])
    def test_unset_means_no_auth(self, value):
        assert resolve_auth_token(value) is None

    def test_value_is_stripped(self):
        assert resolve_auth_token("  abc \n") == "abc"

    @pytest.mark.parametrize("value", ["none", "None", " NONE "])
    def test_none_opts_out_instead_of_being_the_token(self, value):
        assert resolve_auth_token(value) is None


class TestRequireAuthOffLoopback:
    @pytest.mark.parametrize("host", ["127.0.0.1", "localhost", "::1"])
    @pytest.mark.parametrize("value", [None, "", "none", "secret"])
    def test_loopback_never_needs_a_token(self, host, value):
        require_auth_off_loopback(host, value)

    @pytest.mark.parametrize("host", ["0.0.0.0", "192.168.1.5", "::", "myhost.lan"])
    @pytest.mark.parametrize("value", [None, "", "   "])
    def test_network_bind_without_token_is_refused(self, host, value):
        with pytest.raises(ValueError, match="WHATSAPP_MCP_AUTH_TOKEN"):
            require_auth_off_loopback(host, value)

    @pytest.mark.parametrize("value", ["secret", "none"])
    def test_network_bind_with_token_or_opt_out_is_allowed(self, value):
        require_auth_off_loopback("0.0.0.0", value)


@pytest.mark.parametrize("kind", ["http", "sse"])
class TestBearerTokenMiddleware:
    def test_missing_token_is_rejected(self, kind):
        assert _status(kind, None) == 401

    def test_wrong_token_is_rejected(self, kind):
        assert _status(kind, "Bearer nope") == 401

    def test_wrong_scheme_is_rejected(self, kind):
        assert _status(kind, f"Basic {TOKEN}") == 401

    def test_token_without_scheme_is_rejected(self, kind):
        assert _status(kind, TOKEN) == 401

    def test_non_ascii_token_is_rejected_not_crashed(self, kind):
        app, path = _app(kind)
        with TestClient(app) as client:
            response = client.post(
                path, headers={"Host": "localhost:8000", "Authorization": "Bearer caf\u00e9".encode("latin-1")}
            )
        assert response.status_code == 401

    def test_good_token_passes_through(self, kind):
        assert _status(kind, f"Bearer {TOKEN}") != 401

    def test_failed_attempt_is_logged_with_client_ip_and_no_token(self, kind, capsys):
        _status(kind, "Bearer wrong-secret")
        err = capsys.readouterr().err
        assert err.count("rejected") == 1
        assert "testclient" in err
        assert "wrong-secret" not in err and TOKEN not in err


def test_good_token_completes_a_real_initialize_handshake():
    """Proves the lifespan (session manager) still starts behind the middleware."""
    app, path = _app("http")
    headers = {
        "Host": "localhost:8000",
        "Accept": "application/json, text/event-stream",
        "Authorization": f"Bearer {TOKEN}",
    }
    body = {
        "jsonrpc": "2.0",
        "id": 1,
        "method": "initialize",
        "params": {"protocolVersion": "2025-03-26", "capabilities": {}, "clientInfo": {"name": "t", "version": "0"}},
    }
    with TestClient(app) as client:
        response = client.post(path, headers=headers, json=body)
    assert response.status_code == 200
    assert "serverInfo" in response.text


async def test_websocket_scope_fails_closed():
    reached = []
    sent = []

    async def inner(scope, receive, send):
        reached.append(scope["type"])

    async def send(message):
        sent.append(message)

    middleware = BearerTokenMiddleware(inner, token=TOKEN)
    headers = [(b"authorization", f"Bearer {TOKEN}".encode())]
    await middleware({"type": "websocket", "path": "/", "headers": headers, "client": ("10.0.0.9", 1)}, None, send)

    assert reached == []
    assert sent == [{"type": "websocket.close", "code": 1008}]


async def test_lifespan_scope_passes_through():
    reached = []

    async def inner(scope, receive, send):
        reached.append(scope["type"])

    await BearerTokenMiddleware(inner, token=TOKEN)({"type": "lifespan"}, None, None)

    assert reached == ["lifespan"]

    def test_scheme_is_case_insensitive(self, kind):
        assert _status(kind, f"bearer {TOKEN}") != 401

    def test_rejection_names_bearer_scheme(self, kind):
        app, path = _app(kind)
        with TestClient(app) as client:
            response = client.post(path, headers={"Host": "localhost:8000"})
        assert response.headers["www-authenticate"] == "Bearer"
