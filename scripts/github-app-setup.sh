#!/usr/bin/env bash
set -euo pipefail

PORT="${AUTO_BOT_GITHUB_APP_SETUP_PORT:-3219}"
REPO="${GITHUB_DEFAULT_REPO:-}"
if [ -z "$REPO" ]; then
  REMOTE_URL="$(git remote get-url origin 2>/dev/null || true)"
  REPO="$(printf '%s' "$REMOTE_URL" | sed -E 's#^git@github.com:##; s#^https://github.com/##; s#\.git$##')"
fi
if ! printf '%s' "$REPO" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$'; then
  echo "Could not determine GitHub repo in owner/name form. Set GITHUB_DEFAULT_REPO." >&2
  exit 1
fi

OWNER="${REPO%%/*}"
APP_ACCOUNT="${AUTO_BOT_GITHUB_APP_ACCOUNT:-$USER}"
BASE_URL="http://127.0.0.1:${PORT}"
APP_NAME="autobot-${OWNER}-$(date +%m%d%H%M)"

echo "Starting GitHub App setup helper on ${BASE_URL}"
echo "Target repo allowlist: ${REPO}"
echo "Keychain account: ${APP_ACCOUNT}"

AUTO_BOT_GITHUB_APP_SETUP_PORT="$PORT" \
AUTO_BOT_GITHUB_APP_SETUP_REPO="$REPO" \
AUTO_BOT_GITHUB_APP_SETUP_APP_NAME="$APP_NAME" \
AUTO_BOT_GITHUB_APP_ACCOUNT="$APP_ACCOUNT" \
node <<'NODE'
const http = require("http");
const crypto = require("crypto");
const { spawnSync } = require("child_process");

const port = Number(process.env.AUTO_BOT_GITHUB_APP_SETUP_PORT || "3219");
const repo = process.env.AUTO_BOT_GITHUB_APP_SETUP_REPO;
const appName = process.env.AUTO_BOT_GITHUB_APP_SETUP_APP_NAME;
const account = process.env.AUTO_BOT_GITHUB_APP_ACCOUNT || process.env.USER || "auto-bot";
const baseURL = `http://127.0.0.1:${port}`;
const state = crypto.randomBytes(24).toString("hex");

let appSlug = "";
let appID = "";
let storedPrivateKey = false;
let installationID = "";

function html(body) {
  return `<!doctype html><html><head><meta charset="utf-8"><title>Auto Bot GitHub App Setup</title><style>
    body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;max-width:820px;margin:48px auto;padding:0 24px;line-height:1.5;color:#17202a}
    code{background:#f2f4f7;padding:2px 6px;border-radius:5px}
    button,a.button{display:inline-block;border:0;border-radius:8px;background:#0969da;color:white;padding:12px 16px;font-weight:700;text-decoration:none;cursor:pointer}
    .box{border:1px solid #d0d7de;border-radius:10px;padding:18px;margin:18px 0;background:#fff}
    .muted{color:#667085}
    .ok{color:#12805c;font-weight:700}
    .warn{color:#9a6700;font-weight:700}
  </style></head><body>${body}</body></html>`;
}

function write(res, status, body) {
  res.writeHead(status, {"content-type": "text/html; charset=utf-8"});
  res.end(html(body));
}

function storeSecret(service, value) {
  value = String(value || "").replace(/\n/g, "\\n");
  const result = spawnSync("security", [
    "add-generic-password",
    "-U",
    "-s", service,
    "-a", account,
    "-w", value,
  ], { encoding: "utf8" });
  if (result.status !== 0) {
    throw new Error(`security add-generic-password failed for ${service}: ${result.stderr || result.stdout}`);
  }
}

async function postJSON(url, payload) {
  const response = await fetch(url, {
    method: "POST",
    headers: {
      "accept": "application/vnd.github+json",
      "content-type": "application/json",
      "x-github-api-version": "2022-11-28",
    },
    body: JSON.stringify(payload || {}),
  });
  const text = await response.text();
  let data = {};
  try { data = text ? JSON.parse(text) : {}; } catch (_) {}
  if (!response.ok) {
    throw new Error(`GitHub ${response.status}: ${text.slice(0, 1000)}`);
  }
  return data;
}

const manifest = {
  name: appName,
  url: baseURL,
  redirect_url: `${baseURL}/callback`,
  callback_urls: [`${baseURL}/setup`],
  setup_url: `${baseURL}/setup`,
  public: false,
  default_permissions: {
    contents: "read",
    pull_requests: "write",
  },
  default_events: [],
  request_oauth_on_install: false,
  setup_on_update: true,
};

function homePage() {
  return `<h1>Auto Bot GitHub App Setup</h1>
  <div class="box">
    <p>This creates a private GitHub App for <code>${repo}</code> with the minimum runtime permissions Auto Bot needs:</p>
    <ul>
      <li><code>Contents: read</code> to read PR diffs</li>
      <li><code>Metadata: read</code> for repository metadata</li>
      <li><code>Pull requests: write</code> only so Auto Bot can post PR review comments when explicitly enabled</li>
    </ul>
    <p>The app private key and installation ID will be stored in macOS Keychain under account <code>${account}</code>. No <code>.env</code> file is created.</p>
  </div>
  <form action="https://github.com/settings/apps/new?state=${state}" method="post">
    <input type="hidden" name="manifest" value='${JSON.stringify(manifest).replace(/'/g, "&#39;")}'>
    <button type="submit">Create GitHub App</button>
  </form>`;
}

function donePage() {
  const installURL = appSlug ? `https://github.com/apps/${appSlug}/installations/new` : "";
  return `<h1>Auto Bot GitHub App Setup</h1>
  <div class="box">
    <p>App ID stored: <span class="ok">${appID ? "yes" : "not yet"}</span></p>
    <p>Private key stored: <span class="ok">${storedPrivateKey ? "yes" : "not yet"}</span></p>
    <p>Installation ID stored: <span class="${installationID ? "ok" : "warn"}">${installationID ? installationID : "not yet"}</span></p>
  </div>
  ${installationID ? `<p class="ok">GitHub App setup is complete.</p><p>You can close this tab.</p>` : `<p>Next, install the app on exactly <code>${repo}</code>.</p><p><a class="button" href="${installURL}">Install on repository</a></p><p class="muted">Choose "Only select repositories" and select <code>${repo}</code>.</p>`}`;
}

const server = http.createServer(async (req, res) => {
  try {
    const requestURL = new URL(req.url, baseURL);
    if (requestURL.pathname === "/") {
      write(res, 200, homePage());
      return;
    }
    if (requestURL.pathname === "/callback") {
      if (requestURL.searchParams.get("state") !== state) {
        write(res, 400, `<h1>State mismatch</h1><p>Restart the setup helper and try again.</p>`);
        return;
      }
      const code = requestURL.searchParams.get("code");
      if (!code) {
        write(res, 400, `<h1>Missing code</h1>`);
        return;
      }
      const conversion = await postJSON(`https://api.github.com/app-manifests/${encodeURIComponent(code)}/conversions`, {});
      appID = String(conversion.id || "");
      appSlug = String(conversion.slug || "");
      if (!appID || !conversion.pem) {
        throw new Error("GitHub conversion did not return app id and private key");
      }
      storeSecret("auto-bot/github-app-id", appID);
      storeSecret("auto-bot/github-app-private-key", conversion.pem);
      storedPrivateKey = true;
      write(res, 200, donePage());
      return;
    }
    if (requestURL.pathname === "/setup") {
      const id = requestURL.searchParams.get("installation_id");
      if (id) {
        installationID = id;
        storeSecret("auto-bot/github-app-installation-id", installationID);
      }
      write(res, 200, donePage());
      return;
    }
    if (requestURL.pathname === "/status") {
      res.writeHead(200, {"content-type": "application/json"});
      res.end(JSON.stringify({ app_id: !!appID, private_key: storedPrivateKey, installation_id: !!installationID, app_slug: appSlug }, null, 2));
      return;
    }
    write(res, 404, `<h1>Not found</h1>`);
  } catch (error) {
    write(res, 500, `<h1>Setup failed</h1><pre>${String(error.stack || error).replace(/[<>&]/g, c => ({ "<": "&lt;", ">": "&gt;", "&": "&amp;" }[c]))}</pre>`);
  }
});

server.listen(port, "127.0.0.1", () => {
  console.log(`Open ${baseURL} to create and install the GitHub App.`);
  const opener = process.platform === "darwin" ? "open" : "xdg-open";
  spawnSync(opener, [baseURL], { stdio: "ignore" });
});
NODE
