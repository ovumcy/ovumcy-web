import { expect, test, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';

function shiftISODate(iso: string, days: number): string {
  const [y, m, d] = iso.split('-').map((part) => Number(part));
  const date = new Date(y, m - 1, d);
  date.setDate(date.getDate() + days);

  const yyyy = date.getFullYear();
  const mm = String(date.getMonth() + 1).padStart(2, '0');
  const dd = String(date.getDate()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd}`;
}

async function setClientTimezoneCookie(page: Page): Promise<void> {
  const timezone = await page.evaluate(() => {
    try {
      return String(Intl.DateTimeFormat().resolvedOptions().timeZone || '').trim();
    } catch {
      return '';
    }
  });

  if (!timezone) {
    return;
  }

  const origin = new URL(page.url()).origin;
  await page.context().addCookies([
    {
      name: 'ovumcy_tz',
      value: timezone,
      url: origin,
      sameSite: 'Lax',
    },
  ]);
}

async function registerOwnerOnCalendar(page: Page, prefix: string): Promise<void> {
  const creds = createCredentials(prefix);

  await registerOwnerViaUI(page, creds);
  await expect(page).toHaveURL(/\/recovery-code$/);

  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await setClientTimezoneCookie(page);
  await page.goto('/calendar');
  await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);
}

async function todayISOFromCalendar(page: Page): Promise<string> {
  const todayButton = page.locator('button[data-day]:has(.calendar-today-pill)').first();
  await expect(todayButton).toBeVisible();
  const todayISO = await todayButton.getAttribute('data-day');
  expect(todayISO).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  return todayISO!;
}

test.describe('Calendar page', () => {
  test('default month renders and navigation prev/next/today works', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-nav');

    const topCard = page.locator('section.space-y-6 > div.journal-card').first();
    const monthLabel = topCard.locator('p.journal-muted').first();
    const prevLink = topCard.locator('a.btn-secondary').first();
    const nextLink = topCard.locator('a.btn-secondary').nth(1);
    const todayLink = topCard.locator('a.btn-primary[href="/calendar"]');

    const initialLabel = ((await monthLabel.textContent()) ?? '').trim();
    expect(initialLabel.length).toBeGreaterThan(0);

    await prevLink.click();
    await expect(page).toHaveURL(/\/calendar\?month=\d{4}-\d{2}/);
    const prevLabel = ((await monthLabel.textContent()) ?? '').trim();
    expect(prevLabel).not.toBe(initialLabel);

    await nextLink.click();
    await expect(page).toHaveURL(/\/calendar\?month=\d{4}-\d{2}/);

    await todayLink.click();
    await expect(page).toHaveURL(/\/calendar$/);
    await expect(page.locator('button[data-day]:has(.calendar-today-pill)')).toHaveCount(1);
  });

  test('legend includes period/predicted/fertility/ovulation markers', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-legend');

    await expect(page.locator('.legend-dot.legend-dot-period')).toHaveCount(1);
    await expect(page.locator('.legend-dot.legend-dot-predicted')).toHaveCount(1);
    await expect(page.locator('.legend-dot.legend-dot-fertile')).toHaveCount(1);
    await expect(page.locator('.legend-item')).toContainText(['🌞']);
  });

  test('past day entry can be edited from calendar and persists after reload', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-past-edit');

    const todayISO = await todayISOFromCalendar(page);
    const pastISO = shiftISODate(todayISO, -2);
    const pastMonth = pastISO.slice(0, 7);

    await page.goto(`/calendar?month=${pastMonth}&day=${pastISO}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${pastMonth}&day=${pastISO}`));

    const dayEditorForm = page.locator(`form.calendar-day-editor-form[hx-post="/api/days/${pastISO}"]`);
    await expect(dayEditorForm).toBeVisible();

    await dayEditorForm.locator('input[name="is_period"]').check();
    await dayEditorForm.locator('input[name="flow"][value="medium"]').check({ force: true });

    const noteText = `calendar-note-${Date.now()}`;
    await dayEditorForm.locator('#calendar-notes').fill(noteText);
    await dayEditorForm.locator('button[data-save-button]').click();

    await page.goto(`/calendar?month=${pastMonth}&day=${pastISO}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${pastMonth}&day=${pastISO}`));
    await expect(page.locator(`form.calendar-day-editor-form[hx-post="/api/days/${pastISO}"] #calendar-notes`)).toHaveValue(noteText);
    await expect(page.locator(`form.calendar-day-editor-form[hx-post="/api/days/${pastISO}"] input[name="is_period"]`)).toBeChecked();
  });

  test('future day panel shows future warning context', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-future-day');

    const todayISO = await todayISOFromCalendar(page);
    const futureISO = shiftISODate(todayISO, 3);
    const futureMonth = futureISO.slice(0, 7);

    await page.goto(`/calendar?month=${futureMonth}&day=${futureISO}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${futureMonth}&day=${futureISO}`));

    const dayEditorForm = page.locator(`form.calendar-day-editor-form[hx-post="/api/days/${futureISO}"]`);
    await expect(dayEditorForm).toBeVisible();

    const warningPanel = page.locator('#day-editor .journal-panel.text-sm').first();
    await expect(warningPanel).toBeVisible();
    await expect(warningPanel).not.toHaveText(/^$/);
  });

  test('language switch preserves selected month/day query and visible editor', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-lang-query');

    const todayISO = await todayISOFromCalendar(page);
    const pastISO = shiftISODate(todayISO, -2);
    const pastMonth = pastISO.slice(0, 7);

    await page.goto(`/calendar?month=${pastMonth}&day=${pastISO}`);
    await expect(page.locator(`form.calendar-day-editor-form[hx-post="/api/days/${pastISO}"]`)).toBeVisible();

    await page.locator('.lang-switch a[href^="/lang/ru"]').click();
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru');

    const currentURL = new URL(page.url());
    expect(currentURL.pathname).toBe('/calendar');
    expect(currentURL.searchParams.get('month')).toBe(pastMonth);
    expect(currentURL.searchParams.get('day')).toBe(pastISO);
    await expect(page.locator(`form.calendar-day-editor-form[hx-post="/api/days/${pastISO}"]`)).toBeVisible();
  });
});
