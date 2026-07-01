import { expect, test, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';

const IMPORT_SECTION = '[data-import-section]';

type ExportEntry = { date?: string };
type ExportPayload = { entries?: ExportEntry[] };

async function registerOwnerAndOpenSettings(page: Page, prefix: string) {
  const creds = createCredentials(prefix);

  await registerOwnerViaUI(page, creds);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);
  return creds;
}

function exportFileBuffer(entries: unknown): Buffer {
  return Buffer.from(JSON.stringify({ exported_at: new Date().toISOString(), entries }));
}

async function chooseImportFile(
  page: Page,
  name: string,
  buffer: Buffer,
  mimeType = 'application/json',
): Promise<void> {
  await page
    .locator(`${IMPORT_SECTION} [data-import-file]`)
    .setInputFiles({ name, mimeType, buffer });
}

async function submitImport(page: Page): Promise<void> {
  await page.locator(`${IMPORT_SECTION} [data-import-submit]`).click();
}

function lastToast(page: Page) {
  return page.locator('.toast-stack .toast-message').last();
}

async function exportedDates(page: Page): Promise<string[]> {
  const response = await page.request.get('/api/v1/exports/json');
  expect(response.ok()).toBeTruthy();
  const payload = (await response.json()) as ExportPayload;
  return (payload.entries ?? []).map((entry) => String(entry.date ?? ''));
}

test.describe('Settings: restore from JSON backup', () => {
  test('owner restores a valid export and the imported day is created', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'settings-import-happy');

    const file = exportFileBuffer([
      { date: '2026-08-01', period: true, flow: 'medium', cycle_factors: [] },
      { date: '2026-08-02', period: false, cycle_factors: [] },
    ]);
    await chooseImportFile(page, 'ovumcy-export.json', file);
    await submitImport(page);

    await expect(lastToast(page)).toBeVisible();

    // End-to-end effect (locale-independent): the imported day now round-trips
    // back out through the export the restore is the inverse of.
    expect(await exportedDates(page)).toContain('2026-08-01');
  });

  test('malformed file surfaces a toast without crashing the page', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'settings-import-malformed');

    await chooseImportFile(page, 'broken.json', Buffer.from('{not valid json'));
    await submitImport(page);

    await expect(lastToast(page)).toBeVisible();
    // The page stays interactive (no navigation / crash on the 400).
    await expect(page.locator(IMPORT_SECTION)).toBeVisible();
  });

  test('submitting with no file selected shows a prompt and imports nothing', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'settings-import-empty');
    const before = await exportedDates(page);

    await submitImport(page);

    await expect(lastToast(page)).toBeVisible();
    expect(await exportedDates(page)).toEqual(before);
  });

  test('re-importing the same export skips existing days (no duplicate, no overwrite)', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-import-skip');
    const file = exportFileBuffer([
      { date: '2026-09-01', period: true, flow: 'light', cycle_factors: [] },
    ]);

    await chooseImportFile(page, 'export.json', file);
    await submitImport(page);
    await expect(lastToast(page)).toBeVisible();
    expect(await exportedDates(page)).toContain('2026-09-01');

    await chooseImportFile(page, 'export.json', file);
    await submitImport(page);
    await expect(lastToast(page)).toBeVisible();

    // Skip-existing: the day is present exactly once, never duplicated.
    const occurrences = (await exportedDates(page)).filter((date) => date === '2026-09-01').length;
    expect(occurrences).toBe(1);
  });
});
