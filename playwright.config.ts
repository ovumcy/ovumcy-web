import { defineConfig, devices } from '@playwright/test';

const envWorkers = Number.parseInt(process.env.PLAYWRIGHT_WORKERS ?? '', 10);
const workers = Number.isInteger(envWorkers) && envWorkers > 0 ? envWorkers : undefined;
const isCI = !!process.env.CI;

export default defineConfig({
  testDir: 'e2e',
  timeout: 30 * 1000,
  workers,
  // Fail CI if a `.only` was left in a spec, and retry flaky tests on CI only so
  // local runs surface flakiness instead of hiding it behind retries.
  forbidOnly: isCI,
  retries: isCI ? 2 : 0,
  // A test that failed and then passed on retry is NOT a pass. With `retries`
  // above and this left at its default, CI reported such a run green and the
  // flake was visible only to whoever opened the report — which is the same
  // false green the suite guards against everywhere else. Scoped to CI because
  // `retries` is: with 0 retries locally, nothing can be flaky by this
  // definition, so the flag would never fire there anyway.
  failOnFlakyTests: isCI,
  use: {
    baseURL: process.env.PLAYWRIGHT_BASE_URL ?? 'http://localhost:8080',
    headless: true,
    ignoreHTTPSErrors: process.env.PLAYWRIGHT_IGNORE_HTTPS_ERRORS === 'true',
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
    video: 'retain-on-failure',
  },
  // Three projects are DEFINED here; the gating pipeline does not run three.
  // Every `e2e*` script pins `--project=chromium`, and the sharded CI job
  // installs chromium alone, so the full suite is a chromium result. Firefox
  // and WebKit are exercised by `npm run e2e:cross-browser`, which is scoped
  // to `e2e/cross-browser-smoke.spec.ts`. Running all specs on all three would
  // roughly triple e2e wall-clock, so the narrow lane is the accepted trade —
  // stated here because this list on its own reads like coverage that exists.
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
    {
      name: 'firefox',
      // The app sends COOP same-origin, so a fresh context's first navigation swaps the
      // browsing context, and Playwright's firefox (<= r1543, @playwright/test 1.63) can
      // reuse a stale juggler channel and never settle the goto: microsoft/playwright#42731,
      // fixed in r1551. Drop this pref once the installed firefox is r1551 or later.
      // COOP is still enforced by chromium and webkit here, and the header is pinned in Go.
      use: {
        ...devices['Desktop Firefox'],
        launchOptions: {
          firefoxUserPrefs: { 'browser.tabs.remote.useCrossOriginOpenerPolicy': false },
        },
      },
    },
    {
      name: 'webkit',
      use: { ...devices['Desktop Safari'] },
    },
  ],
});
