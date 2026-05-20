#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${AUTO_BOT_LOCAL_AWS_REFRESH_PORT:-38751}"
PID_FILE="${AUTO_BOT_LOCAL_AWS_REFRESH_PID_FILE:-/tmp/auto-bot-local-aws-refresh-broker.pid}"
LOG_FILE="${AUTO_BOT_LOCAL_AWS_REFRESH_LOG_FILE:-/tmp/auto-bot-local-aws-refresh-broker.log}"
TOKEN="${AUTO_BOT_LOCAL_AWS_REFRESH_TOKEN:-}"
LAUNCHD_LABEL="${AUTO_BOT_LOCAL_AWS_REFRESH_LABEL:-com.auto-bot.local-runtime-restart-broker}"

if [ -z "$TOKEN" ]; then
  echo "AUTO_BOT_LOCAL_AWS_REFRESH_TOKEN is required" >&2
  exit 1
fi

is_healthy() {
  curl -fsS "http://127.0.0.1:${PORT}/healthz" >/dev/null 2>&1
}

stop_existing() {
  if [ "$(uname -s)" = "Darwin" ] && command -v launchctl >/dev/null 2>&1; then
    launchctl remove "$LAUNCHD_LABEL" >/dev/null 2>&1 || true
  fi
  if [ -f "$PID_FILE" ]; then
    local pid
    pid="$(cat "$PID_FILE" 2>/dev/null || true)"
    if [ -n "$pid" ] && kill -0 "$pid" >/dev/null 2>&1; then
      kill "$pid" >/dev/null 2>&1 || true
      sleep 0.5
    fi
    rm -f "$PID_FILE"
  fi
}

case "${1:-start}" in
  start)
    if is_healthy; then
      exit 0
    fi
    stop_existing
    if [ "$(uname -s)" = "Darwin" ] && command -v launchctl >/dev/null 2>&1; then
      launchctl submit -l "$LAUNCHD_LABEL" -- /usr/bin/env \
        AUTO_BOT_ROOT_DIR="$ROOT_DIR" \
        AUTO_BOT_LOCAL_AWS_REFRESH_TOKEN="$TOKEN" \
        AUTO_BOT_LOCAL_AWS_REFRESH_PORT="$PORT" \
        AUTO_BOT_LOCAL_AWS_REFRESH_BIND="${AUTO_BOT_LOCAL_AWS_REFRESH_BIND:-127.0.0.1}" \
        AWS_PROFILE="${AWS_PROFILE:-test_AccountA/AdministratorAccess}" \
        AWS_REGION="${AWS_REGION:-us-east-1}" \
        AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-${AWS_REGION:-us-east-1}}" \
        PATH="${PATH:-/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:/Applications/Docker.app/Contents/Resources/bin}" \
        python3 "$ROOT_DIR/scripts/local-aws-refresh-broker.py" >>"$LOG_FILE" 2>&1
    else
      AUTO_BOT_ROOT_DIR="$ROOT_DIR" \
      AUTO_BOT_LOCAL_AWS_REFRESH_TOKEN="$TOKEN" \
      AUTO_BOT_LOCAL_AWS_REFRESH_PORT="$PORT" \
      AUTO_BOT_LOCAL_AWS_REFRESH_BIND="${AUTO_BOT_LOCAL_AWS_REFRESH_BIND:-127.0.0.1}" \
      AWS_PROFILE="${AWS_PROFILE:-test_AccountA/AdministratorAccess}" \
      AWS_REGION="${AWS_REGION:-us-east-1}" \
      AWS_DEFAULT_REGION="${AWS_DEFAULT_REGION:-${AWS_REGION:-us-east-1}}" \
        nohup python3 "$ROOT_DIR/scripts/local-aws-refresh-broker.py" >>"$LOG_FILE" 2>&1 &
      echo "$!" >"$PID_FILE"
    fi
    for _ in $(seq 1 40); do
      if is_healthy; then
        echo "Local runtime restart broker is running on 127.0.0.1:${PORT}" >&2
        exit 0
      fi
      sleep 0.25
    done
    echo "Local AWS refresh broker did not become healthy. Logs: $LOG_FILE" >&2
    exit 1
    ;;
  stop)
    stop_existing
    ;;
  status)
    if is_healthy; then
      curl -fsS "http://127.0.0.1:${PORT}/healthz"
      exit 0
    fi
    exit 1
    ;;
  *)
    echo "Usage: $0 [start|stop|status]" >&2
    exit 2
    ;;
esac
