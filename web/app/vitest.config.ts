import { defineConfig } from "vitest/config"
import react from "@vitejs/plugin-react"

// Test-only Vite config. Kept separate from vite.config.ts so the test
// runner doesn't pull in the dev-server proxy block (it would warn on
// the missing target during tests).
export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./vitest.setup.ts"],
    css: false,
    include: ["src/**/*.test.{ts,tsx}"],
  },
})
