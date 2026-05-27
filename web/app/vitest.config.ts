import { defineConfig } from "vitest/config"
import react from "@vitejs/plugin-react"

// Separate from vite.config.ts so the production build pipeline is
// unaffected by the test-only deps.
export default defineConfig({
  plugins: [react()],
  test: {
    environment: "jsdom",
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.test.{ts,tsx}"],
    css: false,
  },
})
