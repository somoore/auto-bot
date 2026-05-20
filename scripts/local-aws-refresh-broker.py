#!/usr/bin/env python3
"""Local-only runtime restart broker for Docker development.

The app container cannot refresh its own environment credentials or restart
itself with a different voice provider. This helper runs on the macOS host,
accepts a token-protected localhost request, and recreates the app container
with the requested local runtime environment.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path


ROOT_DIR = Path(os.environ.get("AUTO_BOT_ROOT_DIR", Path(__file__).resolve().parents[1])).resolve()
TOKEN = os.environ.get("AUTO_BOT_LOCAL_AWS_REFRESH_TOKEN", "").strip()
PROFILE = os.environ.get("AWS_PROFILE", "test_AccountA/AdministratorAccess")
REGION = os.environ.get("AWS_REGION", "us-east-1")
PORT = int(os.environ.get("AUTO_BOT_LOCAL_AWS_REFRESH_PORT", "38751"))
BIND_HOST = os.environ.get("AUTO_BOT_LOCAL_AWS_REFRESH_BIND", "127.0.0.1")
DEFAULT_PATH = "/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:/Applications/Docker.app/Contents/Resources/bin"
DELAY_SECONDS = float(os.environ.get("AUTO_BOT_LOCAL_AWS_REFRESH_DELAY", "1.5"))
TIMEOUT_SECONDS = int(os.environ.get("AUTO_BOT_LOCAL_AWS_REFRESH_TIMEOUT", "240"))

state_lock = threading.Lock()
refresh_running = False
last_result: dict[str, object] = {"ok": True, "message": "No refresh has run yet."}


def json_response(handler: BaseHTTPRequestHandler, status: int, payload: dict[str, object]) -> None:
    body = json.dumps(payload).encode("utf-8")
    handler.send_response(status)
    handler.send_header("Content-Type", "application/json")
    handler.send_header("Content-Length", str(len(body)))
    handler.end_headers()
    handler.wfile.write(body)


def authorized(handler: BaseHTTPRequestHandler) -> bool:
    if not TOKEN:
        return False
    header = handler.headers.get("Authorization", "").strip()
    return header == f"Bearer {TOKEN}"


def normalize_provider(value: object) -> str:
    provider = str(value or "").strip().lower()
    if provider in {"openai", "openai-realtime"}:
        return "openai"
    if provider in {"nova", "nova-sonic", "aws", "aws-nova-sonic"}:
        return "nova-sonic"
    return provider


def read_restart_request(handler: BaseHTTPRequestHandler) -> dict[str, object]:
    length = int(handler.headers.get("Content-Length", "0") or "0")
    if length > 4096:
        raise ValueError("request body too large")
    if length <= 0:
        return {}
    raw = handler.rfile.read(length)
    if not raw:
        return {}
    payload = json.loads(raw.decode("utf-8"))
    if not isinstance(payload, dict):
        return {}
    return payload


def build_refresh_command(request: dict[str, object]) -> list[str]:
    provider = normalize_provider(request.get("voice_provider"))
    if provider == "openai":
        return [str(ROOT_DIR / "scripts" / "dc-up-keychain.sh"), "-d", "app"]
    if shutil.which("zsh"):
        return [
            "zsh",
            "-lic",
            'assume --confirm --region "$AWS_REGION" --exec "$AUTO_BOT_ASSUME_EXEC" "$AWS_PROFILE"',
        ]
    return [str(ROOT_DIR / "scripts" / "dc-up-keychain.sh"), "-d", "app"]


def apply_restart_environment(env: dict[str, str], request: dict[str, object]) -> tuple[str, str]:
    provider = normalize_provider(request.get("voice_provider"))
    model = str(request.get("voice_model") or "").strip()
    if provider:
        env["VOICE_PROVIDER"] = provider
    if provider == "openai":
        env["AUTO_BOT_SKIP_AWS"] = "1"
        if model:
            env["OPENAI_REALTIME_MODEL"] = model
    elif provider == "nova-sonic":
        if model:
            env["NOVA_SONIC_MODEL"] = model
    return provider, model


def run_refresh(request: dict[str, object]) -> None:
    global refresh_running, last_result
    time.sleep(DELAY_SECONDS)
    env = os.environ.copy()
    env["AWS_PROFILE"] = PROFILE
    env["AWS_REGION"] = REGION
    env["AWS_DEFAULT_REGION"] = REGION
    env["COMPOSE_DISABLE_ENV_FILE"] = "1"
    env["AUTO_BOT_OPEN_BROWSER"] = "0"
    env["AUTO_BOT_ASSUME_EXEC"] = f"{ROOT_DIR}/scripts/dc-up-keychain.sh -d app"
    env["PATH"] = os.environ.get("PATH") or DEFAULT_PATH
    provider, model = apply_restart_environment(env, request)
    command = build_refresh_command(request)
    started_at = time.time()
    try:
        completed = subprocess.run(
            command,
            cwd=str(ROOT_DIR),
            env=env,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            timeout=TIMEOUT_SECONDS,
            check=False,
        )
        output = " ".join(completed.stdout.split())[-1200:]
        last_result = {
            "ok": completed.returncode == 0,
            "exit_code": completed.returncode,
            "duration_seconds": round(time.time() - started_at, 2),
            "voice_provider": provider,
            "voice_model": model,
            "requires_reload": completed.returncode == 0,
            "message": output or ("AWS credential refresh completed." if completed.returncode == 0 else "AWS credential refresh failed."),
        }
    except Exception as exc:  # noqa: BLE001 - surfaced to the local operator UI.
        last_result = {
            "ok": False,
            "duration_seconds": round(time.time() - started_at, 2),
            "voice_provider": provider,
            "voice_model": model,
            "message": str(exc),
        }
    finally:
        with state_lock:
            refresh_running = False


class Handler(BaseHTTPRequestHandler):
    server_version = "AutoBotLocalAWSRefresh/1.0"

    def log_message(self, fmt: str, *args: object) -> None:
        print(f"[local-aws-refresh] {self.address_string()} - {fmt % args}", flush=True)

    def do_GET(self) -> None:  # noqa: N802 - BaseHTTPRequestHandler API.
        if self.path.split("?", 1)[0] != "/healthz":
            json_response(self, 404, {"ok": False, "message": "not found"})
            return
        json_response(self, 200, {"ok": True, "running": refresh_running, "last_result": last_result})

    def do_POST(self) -> None:  # noqa: N802 - BaseHTTPRequestHandler API.
        global refresh_running
        if self.path.split("?", 1)[0] != "/refresh":
            json_response(self, 404, {"ok": False, "message": "not found"})
            return
        if not authorized(self):
            json_response(self, 401, {"ok": False, "message": "unauthorized"})
            return
        try:
            request = read_restart_request(self)
        except Exception as exc:  # noqa: BLE001 - local operator gets a concrete setup error.
            json_response(self, 400, {"ok": False, "message": f"invalid restart request: {exc}"})
            return
        provider = normalize_provider(request.get("voice_provider"))
        if provider and provider not in {"openai", "nova-sonic"}:
            json_response(self, 400, {"ok": False, "message": f"unsupported voice provider: {provider}"})
            return
        with state_lock:
            if refresh_running:
                json_response(self, 202, {"ok": True, "started": False, "running": True, "message": "Local app restart is already running."})
                return
            refresh_running = True
        threading.Thread(target=run_refresh, args=(request,), daemon=True).start()
        json_response(self, 202, {
            "ok": True,
            "started": True,
            "running": True,
            "voice_provider": provider,
            "voice_model": str(request.get("voice_model") or "").strip(),
            "requires_reload": True,
            "message": "Local app restart started.",
        })


def main() -> int:
    if not TOKEN:
        raise SystemExit("AUTO_BOT_LOCAL_AWS_REFRESH_TOKEN is required")
    server = ThreadingHTTPServer((BIND_HOST, PORT), Handler)
    print(f"Local runtime restart broker listening on {BIND_HOST}:{PORT}", flush=True)
    server.serve_forever()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
