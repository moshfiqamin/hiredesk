import { defineConfig, devices } from "@playwright/test";

/**
 * Playwright runs against a stack that is already up (`make up && make
 * seed`), the same containers a reviewer runs, rather than starting its own
 * servers: what is tested is what ships.
 */
export default defineConfig({
  testDir: "./e2e",
  // One worker: the flow creates real records, and steps depend on each other.
  workers: 1,
  fullyParallel: false,
  // A retry in CI absorbs a one-off slow container; locally a failure should
  // be seen the first time.
  retries: process.env.CI ? 1 : 0,
  timeout: 120_000,
  expect: { timeout: 10_000 },
  reporter: [["list"], ["html", { open: "never" }]],
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:3000",
    // A trace (DOM snapshots, network, console) only when a test fails, so a
    // failure can be replayed step by step without slowing passing runs.
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },
  projects: [
    { name: "flow", testMatch: /full-flow\.spec\.ts/, use: { ...devices["Desktop Chrome"] } },
    { name: "audit", testMatch: /audit\/.*\.spec\.ts/, use: { ...devices["Desktop Chrome"] } },
  ],
});
