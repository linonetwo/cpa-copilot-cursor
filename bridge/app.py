from __future__ import annotations

import os
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any, Awaitable, Callable

from aiohttp import web

from runtimes import SubscriptionRuntimes
from store import CredentialStore


Handler = Callable[[web.Request], Awaitable[web.StreamResponse]]


def json_error(message: str, *, status: int = 400) -> web.Response:
    return web.json_response({"error": message}, status=status)


def protected(handler: Handler) -> Handler:
    async def wrapped(request: web.Request) -> web.StreamResponse:
        expected = os.getenv("CPA_SUBSCRIPTION_BRIDGE_SECRET", "").strip()
        if expected:
            supplied = request.headers.get("Authorization", "")
            if supplied != f"Bearer {expected}":
                return json_error("unauthorized", status=401)
        try:
            return await handler(request)
        except FileNotFoundError as error:
            return json_error(str(error), status=404)
        except ValueError as error:
            return json_error(str(error), status=400)
        except Exception as error:
            return json_error(str(error), status=502)

    return wrapped


def parse_provider(value: Any) -> str:
    provider = str(value or "").strip().lower()
    if provider not in {"copilot", "cursor"}:
        raise ValueError("provider must be copilot or cursor")
    return provider


async def create_app() -> web.Application:
    data_dir = Path(os.getenv("CPA_SUBSCRIPTION_BRIDGE_DATA", "/data"))
    cursor_binary = Path(os.getenv("CURSOR_AGENT_PATH", "/opt/cursor-agent/cursor-agent"))
    runtimes = SubscriptionRuntimes(CredentialStore(data_dir), cursor_binary)
    app = web.Application(client_max_size=16 << 20)
    app["runtimes"] = runtimes

    async def health(_: web.Request) -> web.Response:
        return web.json_response({"status": "ok"})

    @protected
    async def oauth_start(request: web.Request) -> web.Response:
        payload = await request.json()
        provider = parse_provider(payload.get("provider"))
        url, state, metadata = await runtimes.start_login(provider)
        expires_at = (datetime.now(UTC) + timedelta(minutes=15)).isoformat().replace("+00:00", "Z")
        return web.json_response(
            {"url": url, "state": state, "expires_at": expires_at, "metadata": metadata}
        )

    @protected
    async def oauth_poll(request: web.Request) -> web.Response:
        payload = await request.json()
        provider = parse_provider(payload.get("provider"))
        status, message, auth = await runtimes.poll_login(provider, str(payload.get("state", "")))
        response: dict[str, Any] = {"status": status, "message": message}
        if auth is not None:
            response["auth"] = {
                "type": auth.type,
                "upstream": auth.upstream,
                "handle": auth.handle,
                "label": auth.label,
                "login": auth.login,
                "created_at": auth.created_at,
            }
        return web.json_response(response)

    @protected
    async def models(request: web.Request) -> web.Response:
        payload = await request.json()
        provider = parse_provider(payload.get("provider"))
        handle = str(payload.get("handle", ""))
        return web.json_response({"models": await runtimes.list_models(provider, handle)})

    @protected
    async def execute(request: web.Request) -> web.Response:
        payload = await request.json()
        provider = parse_provider(payload.get("provider"))
        handle = str(payload.get("handle", ""))
        model = str(payload.get("model", ""))
        chat_payload = payload.get("payload")
        if not isinstance(chat_payload, dict):
            raise ValueError("payload must be a chat-completions JSON object")
        response = await runtimes.execute(
            provider,
            handle,
            model,
            chat_payload,
            bool(payload.get("stream")),
        )
        return web.json_response(response)

    @protected
    async def quota(request: web.Request) -> web.Response:
        payload = await request.json()
        provider = parse_provider(payload.get("provider"))
        handle = str(payload.get("handle", ""))
        return web.json_response(await runtimes.quota(provider, handle))

    app.router.add_get("/healthz", health)
    app.router.add_post("/v1/oauth/start", oauth_start)
    app.router.add_post("/v1/oauth/poll", oauth_poll)
    app.router.add_post("/v1/models", models)
    app.router.add_post("/v1/execute", execute)
    app.router.add_post("/v1/quota", quota)
    return app


def main() -> None:
    web.run_app(create_app(), host="127.0.0.1", port=8789, access_log=None)


if __name__ == "__main__":
    main()
