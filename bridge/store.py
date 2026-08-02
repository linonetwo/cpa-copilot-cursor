from __future__ import annotations

import json
import os
import secrets
from dataclasses import asdict, dataclass
from datetime import UTC, datetime
from pathlib import Path


@dataclass(slots=True)
class AuthRecord:
    type: str
    upstream: str
    handle: str
    label: str
    login: str
    created_at: str


class CredentialStore:
    def __init__(self, root: Path) -> None:
        self.root = root
        self.root.mkdir(parents=True, exist_ok=True)

    def new_handle(self) -> str:
        return secrets.token_urlsafe(18).replace("-", "").replace("_", "")

    def account_dir(self, provider: str, handle: str) -> Path:
        self._validate(provider, handle)
        path = self.root / provider / handle
        path.mkdir(parents=True, exist_ok=True)
        return path

    def home_dir(self, provider: str, handle: str) -> Path:
        path = self.account_dir(provider, handle) / "home"
        path.mkdir(parents=True, exist_ok=True)
        return path

    def save_record(self, provider: str, handle: str, *, label: str = "", login: str = "") -> AuthRecord:
        created_at = datetime.now(UTC).isoformat().replace("+00:00", "Z")
        record = AuthRecord(
            type="subscription-bridge",
            upstream=provider,
            handle=handle,
            label=label or f"{provider.title()} subscription",
            login=login,
            created_at=created_at,
        )
        path = self.account_dir(provider, handle) / "auth.json"
        path.write_text(json.dumps(asdict(record), ensure_ascii=False, indent=2), encoding="utf-8")
        try:
            os.chmod(path, 0o600)
        except OSError:
            pass
        return record

    def load_record(self, provider: str, handle: str) -> AuthRecord:
        path = self.account_dir(provider, handle) / "auth.json"
        payload = json.loads(path.read_text(encoding="utf-8"))
        return AuthRecord(**payload)

    def assert_account(self, provider: str, handle: str) -> None:
        self.load_record(provider, handle)

    @staticmethod
    def _validate(provider: str, handle: str) -> None:
        if provider not in {"copilot", "cursor"}:
            raise ValueError("unsupported provider")
        if not handle or not handle.isalnum() or len(handle) > 80:
            raise ValueError("invalid account handle")

