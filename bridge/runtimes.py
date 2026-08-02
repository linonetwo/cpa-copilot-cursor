from __future__ import annotations

import asyncio
import os
import re
import tempfile
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from copilot import CopilotClient
from copilot._cli_download import get_or_download_cli
from copilot.generated.rpc import AccountGetQuotaRequest

from openai_payload import completion_payload, prompt_from_payload, stream_chunks
from store import AuthRecord, CredentialStore


URL_PATTERN = re.compile(r"https://[^\s<>\"']+")
USER_CODE_PATTERN = re.compile(r"\b[A-Z0-9]{4}-[A-Z0-9]{4}\b")


@dataclass(slots=True)
class LoginFlow:
    provider: str
    handle: str
    process: asyncio.subprocess.Process
    output: list[str] = field(default_factory=list)
    url: str = ""
    user_code: str = ""
    reader: asyncio.Task[None] | None = None


class SubscriptionRuntimes:
    def __init__(self, store: CredentialStore, cursor_binary: Path) -> None:
        self.store = store
        self.cursor_binary = cursor_binary
        self.logins: dict[str, LoginFlow] = {}

    async def start_login(self, provider: str) -> tuple[str, str, dict[str, Any]]:
        handle = self.store.new_handle()
        state = self.store.new_handle()
        home = self.store.home_dir(provider, handle)
        command, env = self._login_command(provider, home)
        process = await asyncio.create_subprocess_exec(
            *command,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.STDOUT,
            env=env,
        )
        flow = LoginFlow(provider=provider, handle=handle, process=process)
        flow.reader = asyncio.create_task(self._read_login_output(flow))
        self.logins[state] = flow

        for _ in range(300):
            if flow.url:
                url = flow.url
                if provider == "copilot" and flow.user_code:
                    url = f"{url}?user_code={flow.user_code}"
                return url, state, {
                    "instructions": self._recent_output(flow),
                    "user_code": flow.user_code,
                }
            if process.returncode is not None:
                break
            await asyncio.sleep(0.1)
        await self._stop_flow(flow)
        self.logins.pop(state, None)
        raise RuntimeError(f"{provider} login did not produce an authorization URL: {self._recent_output(flow)}")

    async def poll_login(self, provider: str, state: str) -> tuple[str, str, AuthRecord | None]:
        flow = self.logins.get(state)
        if flow is None or flow.provider != provider:
            return "error", "login state was not found or expired", None
        return_code = flow.process.returncode
        if return_code is None:
            return "pending", self._recent_output(flow) or "waiting for browser authorization", None
        if flow.reader is not None:
            await asyncio.gather(flow.reader, return_exceptions=True)
        self.logins.pop(state, None)
        if return_code != 0:
            return "error", self._recent_output(flow) or f"login exited with code {return_code}", None
        record = self.store.save_record(
            provider,
            flow.handle,
            label=f"{provider.title()} subscription {flow.handle[:6]}",
        )
        return "success", "login completed", record

    async def list_models(self, provider: str, handle: str) -> list[dict[str, Any]]:
        self.store.assert_account(provider, handle)
        if provider == "copilot":
            return await self._copilot_models(handle)
        return await self._cursor_models(handle)

    async def execute(
        self,
        provider: str,
        handle: str,
        model: str,
        payload: dict[str, Any],
        stream: bool,
    ) -> dict[str, Any]:
        self.store.assert_account(provider, handle)
        prompt = prompt_from_payload(payload)
        if provider == "copilot":
            content = await self._copilot_execute(handle, model, prompt)
        else:
            content = await self._cursor_execute(handle, model, prompt)
        if stream:
            return {"payload": {}, "chunks": stream_chunks(model, content)}
        return {"payload": completion_payload(model, content)}

    async def quota(self, provider: str, handle: str) -> dict[str, Any]:
        self.store.assert_account(provider, handle)
        if provider == "copilot":
            home = self.store.home_dir(provider, handle)
            client = self._copilot_client(home)
            await client.start()
            try:
                result = await client.rpc.account.get_quota(AccountGetQuotaRequest())
                return result.to_dict()
            finally:
                await client.stop()
        output = await self._run_cursor(handle, ["status"], timeout=30)
        return {"status": output.strip(), "source": "cursor-agent status"}

    def _login_command(self, provider: str, home: Path) -> tuple[list[str], dict[str, str]]:
        env = os.environ.copy()
        env["HOME"] = str(home)
        env["NO_OPEN_BROWSER"] = "1"
        env["BROWSER"] = "echo"
        env["GH_BROWSER"] = "echo"
        if provider == "copilot":
            env["COPILOT_HOME"] = str(home / ".copilot")
            cli = get_or_download_cli()
            if not cli:
                raise RuntimeError("official Copilot CLI runtime is unavailable")
            return [cli, "login"], env
        if provider == "cursor":
            return [str(self.cursor_binary), "login"], env
        raise ValueError("unsupported provider")

    async def _read_login_output(self, flow: LoginFlow) -> None:
        if flow.process.stdout is None:
            return
        while True:
            line = await flow.process.stdout.readline()
            if not line:
                break
            text = line.decode("utf-8", errors="replace").strip()
            if text:
                flow.output.append(text)
                match = URL_PATTERN.search(text)
                if match and not flow.url:
                    flow.url = match.group(0).rstrip(".,;)")
                code_match = USER_CODE_PATTERN.search(text)
                if code_match and not flow.user_code:
                    flow.user_code = code_match.group(0)
        await flow.process.wait()

    async def _stop_flow(self, flow: LoginFlow) -> None:
        if flow.process.returncode is None:
            flow.process.terminate()
            try:
                await asyncio.wait_for(flow.process.wait(), timeout=3)
            except TimeoutError:
                flow.process.kill()
                await flow.process.wait()
        if flow.reader is not None:
            await asyncio.gather(flow.reader, return_exceptions=True)

    @staticmethod
    def _recent_output(flow: LoginFlow) -> str:
        return "\n".join(flow.output[-12:])[-4000:]

    def _copilot_client(self, home: Path) -> CopilotClient:
        env = os.environ.copy()
        env["HOME"] = str(home)
        env["COPILOT_HOME"] = str(home / ".copilot")
        return CopilotClient(
            base_directory=str(home / ".copilot"),
            env=env,
            use_logged_in_user=True,
            mode="empty",
        )

    async def _copilot_models(self, handle: str) -> list[dict[str, Any]]:
        home = self.store.home_dir("copilot", handle)
        client = self._copilot_client(home)
        await client.start()
        try:
            models = await client.list_models()
            return [
                {
                    "id": model.id,
                    "display_name": getattr(model, "name", None) or model.id,
                    "description": getattr(model, "description", None) or "GitHub Copilot model",
                    "context_length": getattr(model, "max_context_window_tokens", None) or 0,
                    "max_completion_tokens": getattr(model, "max_output_tokens", None) or 0,
                }
                for model in models
            ]
        finally:
            await client.stop()

    async def _copilot_execute(self, handle: str, model: str, prompt: str) -> str:
        home = self.store.home_dir("copilot", handle)
        client = self._copilot_client(home)
        await client.start()
        session = None
        try:
            session = await client.create_session(
                model=model or None,
                tools=[],
                available_tools=[],
                working_directory=str(home),
                skip_custom_instructions=True,
            )
            message = await session.send_and_wait(prompt, timeout=170)
            return message.data.content if message is not None else ""
        finally:
            if session is not None:
                await session.disconnect()
            await client.stop()

    async def _cursor_models(self, handle: str) -> list[dict[str, Any]]:
        output = await self._run_cursor(handle, ["--list-models"], timeout=30)
        models: list[dict[str, Any]] = []
        for line in output.splitlines():
            value = line.strip().lstrip("-* ").strip()
            if not value or "model" in value.lower() and ":" not in value:
                continue
            model_id = value.split()[0]
            if re.fullmatch(r"[A-Za-z0-9._:/-]+", model_id):
                models.append({"id": model_id, "display_name": value})
        if not models:
            models.append({"id": "auto", "display_name": "Auto"})
        return models

    async def _cursor_execute(self, handle: str, model: str, prompt: str) -> str:
        with tempfile.TemporaryDirectory(prefix="cpa-cursor-") as workspace:
            args = [
                "--print",
                "--trust",
                "--mode",
                "ask",
                "--workspace",
                workspace,
                "--output-format",
                "text",
            ]
            if model:
                args.extend(["--model", model])
            args.append(prompt)
            return (await self._run_cursor(handle, args, timeout=170)).strip()

    async def _run_cursor(self, handle: str, args: list[str], *, timeout: int) -> str:
        home = self.store.home_dir("cursor", handle)
        env = os.environ.copy()
        env["HOME"] = str(home)
        process = await asyncio.create_subprocess_exec(
            str(self.cursor_binary),
            *args,
            stdout=asyncio.subprocess.PIPE,
            stderr=asyncio.subprocess.STDOUT,
            env=env,
        )
        try:
            stdout, _ = await asyncio.wait_for(process.communicate(), timeout=timeout)
        except TimeoutError:
            process.kill()
            await process.wait()
            raise RuntimeError("Cursor CLI timed out")
        output = stdout.decode("utf-8", errors="replace")
        if process.returncode != 0:
            raise RuntimeError(f"Cursor CLI exited with {process.returncode}: {output[-4000:]}")
        return output
