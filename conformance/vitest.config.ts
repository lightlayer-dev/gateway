import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    globals: true,
    testTimeout: 30_000,
    hookTimeout: 10_000,
    reporters: ["verbose"],
    env: {
      BASE_URL: process.env.BASE_URL ?? "http://localhost:8080",
    },
  },
});
