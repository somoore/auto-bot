# Auto-Bot v2 — Web App

React + Vite + TypeScript SPA that will progressively replace
`web/index_livekit.html`. The Go server serves the built bundle from
`/app/*`; the legacy HTML continues to serve from `/`.

## Development

```bash
cd web/app
npm install
npm run dev
```

Then open <http://localhost:5173/app/>. The Vite dev server proxies
`/healthz` and `/websocket` to the Go server on `http://localhost:3000`,
so run the backend (`make dev`) in another terminal.

## Production build

```bash
cd web/app
npm ci
npm run build
```

This produces `web/app/dist/`. The Go server serves these files under
`/app/*` automatically if the directory exists; otherwise that route
returns 404 and the rest of the server is unaffected.

## Stack

React 18, Vite 6, TypeScript (strict), Tailwind v3, TanStack Query v5,
livekit-client. Versions are pinned in `package.json`.
