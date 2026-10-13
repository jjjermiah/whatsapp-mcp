"""Tests for the optional bearer-token auth in front of the http/sse transports."""

import pytest
from mcp.server.fastmcp import FastMCP
from starlette.testclient import TestClient

from mcp_auth import BearerTokenMiddleware
from mcp_config import resolve_auth_token

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

    def test_scheme_is_case_insensitive(self, kind):
        assert _status(kind, f"bearer {TOKEN}") != 401

    def test_rejection_names_bearer_scheme(self, kind):
        app, path = _app(kind)
        with TestClient(app) as client:
            response = client.post(path, headers={"Host": "localhost:8000"})
        assert response.headers["www-authenticate"] == "Bearer"
