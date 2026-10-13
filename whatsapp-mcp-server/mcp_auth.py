"""Optional static bearer-token auth for the http/sse MCP transports.

The SDK's built-in auth (FastMCP ``auth`` + ``token_verifier``) is OAuth 2.1
resource-server machinery: it needs an issuer URL, must be wired in at
construction time (before main.py reads any env var) and advertises OAuth
discovery metadata that clients then try to follow. A single shared secret
needs none of that, so this wraps the app FastMCP builds in a small ASGI
middleware instead.
"""

import hmac
import sys
from typing import Any

from starlette.datastructures import Headers
from starlette.responses import JSONResponse


class BearerTokenMiddleware:
    """Reject every request that lacks ``Authorization: Bearer <token>``.

    Only ``lifespan`` (server startup/shutdown, no client) bypasses the check. Any other
    non-HTTP scope, such as a websocket, is refused rather than passed through unchecked.
    """

    def __init__(self, app: Any, token: str) -> None:
        self.app = app
        self._token = token.encode()

    async def __call__(self, scope: Any, receive: Any, send: Any) -> None:
        kind = scope["type"]
        if kind == "lifespan" or (kind == "http" and self._authorized(Headers(scope=scope).get("authorization", ""))):
            await self.app(scope, receive, send)
            return
        client = scope.get("client")
        # The token is never logged: only who asked and for what.
        print(
            f"WARNING: rejected {kind} request to {scope.get('path', '')} from {client[0] if client else 'unknown'}: "
            "missing or invalid bearer token",
            file=sys.stderr,
        )
        if kind != "http":
            if kind == "websocket":
                await send({"type": "websocket.close", "code": 1008})
            return
        response = JSONResponse(
            {"error": "unauthorized", "message": "missing or invalid bearer token"},
            status_code=401,
            headers={"WWW-Authenticate": "Bearer"},
        )
        await response(scope, receive, send)

    def _authorized(self, header: str) -> bool:
        scheme, _, supplied = header.partition(" ")
        # compare_digest keeps the check constant-time; it needs bytes for non-ASCII input.
        return scheme.lower() == "bearer" and hmac.compare_digest(supplied.strip().encode(), self._token)


def run_with_bearer_auth(mcp: Any, transport: str, token: str) -> None:
    """Serve ``mcp`` over http/sse behind BearerTokenMiddleware.

    Mirrors what ``FastMCP.run`` does for these transports (build the Starlette
    app, hand it to uvicorn) with the middleware added in between.
    """
    import uvicorn

    app = mcp.sse_app() if transport == "sse" else mcp.streamable_http_app()
    app.add_middleware(BearerTokenMiddleware, token=token)
    uvicorn.run(app, host=mcp.settings.host, port=mcp.settings.port, log_level=mcp.settings.log_level.lower())
