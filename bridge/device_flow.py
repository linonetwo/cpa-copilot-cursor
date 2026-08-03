from __future__ import annotations

from typing import Any
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit


def authorization_url(provider: str, verification_uri: str, user_code: str) -> str:
    if provider != "copilot" or not user_code:
        return verification_uri

    parts = urlsplit(verification_uri)
    query = parse_qsl(parts.query, keep_blank_values=True)
    query = [(key, value) for key, value in query if key != "user_code"]
    query.append(("user_code", user_code))
    return urlunsplit((parts.scheme, parts.netloc, parts.path, urlencode(query), parts.fragment))


def login_metadata(
    verification_uri: str,
    user_code: str,
    instructions: str,
) -> dict[str, Any]:
    metadata: dict[str, Any] = {
        "instructions": instructions,
        "verification_uri": verification_uri,
    }
    if user_code:
        metadata["user_code"] = user_code
    return metadata
