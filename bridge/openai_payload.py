from __future__ import annotations

import json
import time
import uuid
from typing import Any


def prompt_from_payload(payload: dict[str, Any]) -> str:
    parts: list[str] = []
    for message in payload.get("messages", []):
        if not isinstance(message, dict):
            continue
        role = str(message.get("role", "user")).upper()
        content = flatten_content(message.get("content"))
        if content:
            parts.append(f"{role}:\n{content}")
    if not parts:
        raise ValueError("messages must contain at least one text message")
    parts.append("ASSISTANT:")
    return "\n\n".join(parts)


def flatten_content(content: Any) -> str:
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        texts: list[str] = []
        for item in content:
            if isinstance(item, str):
                texts.append(item)
            elif isinstance(item, dict) and isinstance(item.get("text"), str):
                texts.append(item["text"])
        return "\n".join(texts)
    return ""


def completion_payload(model: str, content: str) -> dict[str, Any]:
    return {
        "id": f"chatcmpl-{uuid.uuid4().hex}",
        "object": "chat.completion",
        "created": int(time.time()),
        "model": model,
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": content},
                "finish_reason": "stop",
            }
        ],
        "usage": {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
    }


def stream_chunks(model: str, content: str) -> list[str]:
    completion_id = f"chatcmpl-{uuid.uuid4().hex}"
    created = int(time.time())
    first = {
        "id": completion_id,
        "object": "chat.completion.chunk",
        "created": created,
        "model": model,
        "choices": [{"index": 0, "delta": {"role": "assistant"}, "finish_reason": None}],
    }
    body = {
        "id": completion_id,
        "object": "chat.completion.chunk",
        "created": created,
        "model": model,
        "choices": [{"index": 0, "delta": {"content": content}, "finish_reason": None}],
    }
    final = {
        "id": completion_id,
        "object": "chat.completion.chunk",
        "created": created,
        "model": model,
        "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}],
    }
    return [
        f"data: {json.dumps(first, ensure_ascii=False)}\n\n",
        f"data: {json.dumps(body, ensure_ascii=False)}\n\n",
        f"data: {json.dumps(final, ensure_ascii=False)}\n\n",
        "data: [DONE]\n\n",
    ]
