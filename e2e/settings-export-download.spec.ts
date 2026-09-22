import fs from 'node:fs/promises';
import { expect, test, type Page } from './support/fixtures';
import {
  apiOriginHeader,
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { isoToday, shiftISODate } from './support/iso-date-helpers';

/**
 * TS-M17 (WEB-17): every export download button, for every supported range,
 * captures the download and checks both the filename and the parsed content —
 * entries inside the chosen range present, entries outside it absent. The API
 * contract itself (status codes, header shape, malformed-range handling) stays
 * tested at its own level (`export_regressions_test.go`); this spec is the one
 * place that presses the actual buttons and reads what lands on disk.
 *
 * Range math rides on the app's own preset semantics (N days ending at the
 * account's latest entry — `computePresetRange` in
 * `web/src/js/settings-export/10-context-range-summary.js`) rather than on a
 * fixed "today": onboarding's auto period fill seeds a few days around
 * `today - 3` (`completeOnboardingIfPresent`), so the preset window's upper
 * edge sits a few days below the runner's wall clock, not exactly on it. The
 * four fixture entries below are placed 20 / 60 / 200 / 500 days before
 * "today", which is 17+ days of slack on every preset boundary (30 / 90 / 365)
 * — comfortably clear of that drift and of any month-length difference
 * (28-31 days) the day arithmetic crosses. No assertion here depends on which
 * calendar month a boundary falls in.
 */

const MARK_WITHIN_30 = 'SEED-MARK-WITHIN-30D';
const MARK_WITHIN_90 = 'SEED-MARK-WITHIN-90D';
const MARK_WITHIN_365 = 'SEED-MARK-WITHIN-365D';
const MARK_WITHIN_ALL = 'SEED-MARK-WITHIN-ALL-ONLY';

interface SeedDates {
  within30: string;
  within90: string;
  within365: string;
  withinAllOnly: string;
}

async function csrfToken(page: Page): Promise<string> {
  return (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
}

// A JSON body replaces the record with exactly what it states (no hidden-field
// preservation applies, unlike a form body — see `hiddenDayFields` in
// `handlers_days_write.go`), so this seeds the same DailyLog fields the export
// service reads regardless of which tracking toggles the account carries.
async function seedDay(page: Page, isoDate: string, notes: string): Promise<void> {
  const response = await page.request.put(`/api/v1/days/${isoDate}`, {
    headers: {
      ...apiOriginHeader(page),
      'X-CSRF-Token': await csrfToken(page),
      'Content-Type': 'application/json',
    },
    data: { is_period: true, flow: 'medium', notes },
  });
  expect(response.status(), `seed day ${isoDate}`).toBeLessThan(400);
}

async function registerOwnerAndSeedExportFixture(page: Page, prefix: string): Promise<SeedDates> {
  const credentials = createCredentials(prefix);
  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);
  await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);

  const today = isoToday();
  const dates: SeedDates = {
    within30: shiftISODate(today, -20),
    within90: shiftISODate(today, -60),
    within365: shiftISODate(today, -200),
    withinAllOnly: shiftISODate(today, -500),
  };

  // Seeded before the settings page below is loaded: the export panel's
  // min/max bounds (data-export-min/max) are rendered server-side from the
  // account's daily_log range at request time, so the earliest/latest seed
  // dates must exist before that page load for the "all" preset to reach them.
  await seedDay(page, dates.within30, MARK_WITHIN_30);
  await seedDay(page, dates.within90, MARK_WITHIN_90);
  await seedDay(page, dates.within365, MARK_WITHIN_365);
  await seedDay(page, dates.withinAllOnly, MARK_WITHIN_ALL);

  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);
  await expect(page.locator('[data-export-section]')).toBeVisible();

  return dates;
}

async function applyPresetAndDownload(page: Page, type: 'csv' | 'json', preset: string) {
  // `applyPreset` writes the range synchronously into the from/to fields and
  // `updatePresetState` marks the matching button `btn-primary` in the same
  // tick (10-context-range-summary.js); there is no aria-pressed attribute.
  // Waiting on the class proves the range this click asked for is the one
  // the export handler will read at click time, not just that the click fired.
  await page.locator(`[data-export-preset="${preset}"]`).click();
  await expect(page.locator(`[data-export-preset="${preset}"]`)).toHaveClass(/btn-primary/);

  const [download] = await Promise.all([
    page.waitForEvent('download'),
    page.locator(`[data-export-action][data-export-type="${type}"]`).click(),
  ]);

  const downloadPath = await download.path();
  expect(downloadPath, `${type} export (${preset}) produced no file`).toBeTruthy();
  const content = await fs.readFile(downloadPath, 'utf8');
  return { download, content };
}

test.describe('Settings: export download buttons', () => {
  test('CSV export: filename and per-preset row content', async ({ page }) => {
    // Registration, onboarding, four API seeds and four sequential
    // preset-then-download round trips comfortably clear the default 30s
    // budget under serial fast-mode CPU contention (observed ~50s here);
    // `test.slow()` is the repo's convention for that (dashboard.spec.ts,
    // calendar.spec.ts), not a bespoke per-test timeout.
    test.slow();
    const dates = await registerOwnerAndSeedExportFixture(page, 'export-csv');

    // "all": every seeded entry is inside the account's own min/max bounds.
    const all = await applyPresetAndDownload(page, 'csv', 'all');
    expect(all.download.suggestedFilename()).toMatch(/^ovumcy-export-\d{4}-\d{2}-\d{2}\.csv$/);
    const headerLine = all.content.split(/\r?\n/, 1)[0];
    expect(headerLine).toBe(
      [
        'Date',
        'Period',
        'Flow',
        'Mood rating',
        'Sex activity',
        'BBT (C)',
        'Cervical mucus',
        'Cramps',
        'Headache',
        'Acne',
        'Mood',
        'Bloating',
        'Fatigue',
        'Breast tenderness',
        'Back pain',
        'Nausea',
        'Spotting',
        'Irritability',
        'Insomnia',
        'Food cravings',
        'Diarrhea',
        'Constipation',
        'Swelling',
        'Cycle factors',
        'Other',
        'Notes',
        'Pregnancy test',
        'Cycle start',
        'Uncertain',
      ].join(',')
    );
    for (const [date, mark] of [
      [dates.within30, MARK_WITHIN_30],
      [dates.within90, MARK_WITHIN_90],
      [dates.within365, MARK_WITHIN_365],
      [dates.withinAllOnly, MARK_WITHIN_ALL],
    ] as const) {
      expect(all.content, `"all" CSV must contain ${mark}`).toContain(mark);
      expect(all.content, `"all" CSV must contain the row date ${date}`).toContain(date);
    }

    // "30": only the entry 20 days back is inside [latest-29, latest].
    const within30 = await applyPresetAndDownload(page, 'csv', '30');
    expect(within30.download.suggestedFilename()).toMatch(/^ovumcy-export-\d{4}-\d{2}-\d{2}\.csv$/);
    expect(within30.content).toContain(MARK_WITHIN_30);
    expect(within30.content).not.toContain(MARK_WITHIN_90);
    expect(within30.content).not.toContain(MARK_WITHIN_365);
    expect(within30.content).not.toContain(MARK_WITHIN_ALL);

    // "90": adds the entry 60 days back, still excludes 200/500 days back.
    const within90 = await applyPresetAndDownload(page, 'csv', '90');
    expect(within90.content).toContain(MARK_WITHIN_30);
    expect(within90.content).toContain(MARK_WITHIN_90);
    expect(within90.content).not.toContain(MARK_WITHIN_365);
    expect(within90.content).not.toContain(MARK_WITHIN_ALL);

    // "365": adds the entry 200 days back, still excludes the 500-day one.
    const within365 = await applyPresetAndDownload(page, 'csv', '365');
    expect(within365.content).toContain(MARK_WITHIN_30);
    expect(within365.content).toContain(MARK_WITHIN_90);
    expect(within365.content).toContain(MARK_WITHIN_365);
    expect(within365.content).not.toContain(MARK_WITHIN_ALL);
  });

  test('JSON export: filename and per-preset entry content', async ({ page }) => {
    test.slow();
    const dates = await registerOwnerAndSeedExportFixture(page, 'export-json');

    const all = await applyPresetAndDownload(page, 'json', 'all');
    expect(all.download.suggestedFilename()).toMatch(/^ovumcy-export-\d{4}-\d{2}-\d{2}\.json$/);
    const allPayload = JSON.parse(all.content) as { entries: Array<{ date: string; notes: string }> };
    expect(Array.isArray(allPayload.entries)).toBe(true);
    const byDate = new Map(allPayload.entries.map((entry) => [entry.date, entry]));
    expect(byDate.get(dates.within30)?.notes).toBe(MARK_WITHIN_30);
    expect(byDate.get(dates.within90)?.notes).toBe(MARK_WITHIN_90);
    expect(byDate.get(dates.within365)?.notes).toBe(MARK_WITHIN_365);
    expect(byDate.get(dates.withinAllOnly)?.notes).toBe(MARK_WITHIN_ALL);

    const within30 = await applyPresetAndDownload(page, 'json', '30');
    expect(within30.download.suggestedFilename()).toMatch(/^ovumcy-export-\d{4}-\d{2}-\d{2}\.json$/);
    const within30Payload = JSON.parse(within30.content) as {
      entries: Array<{ date: string; notes: string }>;
    };
    const within30Dates = new Set(within30Payload.entries.map((entry) => entry.date));
    expect(within30Dates.has(dates.within30)).toBe(true);
    expect(within30Dates.has(dates.within90)).toBe(false);
    expect(within30Dates.has(dates.within365)).toBe(false);
    expect(within30Dates.has(dates.withinAllOnly)).toBe(false);

    const within90 = await applyPresetAndDownload(page, 'json', '90');
    const within90Payload = JSON.parse(within90.content) as {
      entries: Array<{ date: string; notes: string }>;
    };
    const within90Dates = new Set(within90Payload.entries.map((entry) => entry.date));
    expect(within90Dates.has(dates.within30)).toBe(true);
    expect(within90Dates.has(dates.within90)).toBe(true);
    expect(within90Dates.has(dates.within365)).toBe(false);
    expect(within90Dates.has(dates.withinAllOnly)).toBe(false);

    const within365 = await applyPresetAndDownload(page, 'json', '365');
    const within365Payload = JSON.parse(within365.content) as {
      entries: Array<{ date: string; notes: string }>;
    };
    const within365Dates = new Set(within365Payload.entries.map((entry) => entry.date));
    expect(within365Dates.has(dates.within30)).toBe(true);
    expect(within365Dates.has(dates.within90)).toBe(true);
    expect(within365Dates.has(dates.within365)).toBe(true);
    expect(within365Dates.has(dates.withinAllOnly)).toBe(false);
  });
});
