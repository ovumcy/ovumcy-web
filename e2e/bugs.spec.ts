import { expect, test, type Page } from '@playwright/test';
import { fillDateField } from './support/date-field-helpers';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
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

async function registerOwnerAndReachDashboard(page: Page, prefix: string) {
  const credentials = createCredentials(prefix);

  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);
  await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);

  return credentials;
}

async function pickTimezoneWithDifferentUTCDate(page: Page): Promise<string> {
  return page.evaluate(() => {
    const now = new Date();
    const formatter = new Intl.DateTimeFormat('en-CA', {
      timeZone: 'UTC',
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    });
    const utcDate = formatter.format(now);
    const candidates = [
      'Pacific/Kiritimati',
      'Pacific/Pago_Pago',
      'Pacific/Auckland',
      'America/Adak',
      'Europe/Moscow',
    ];

    for (const timezone of candidates) {
      try {
        const localDate = new Intl.DateTimeFormat('en-CA', {
          timeZone: timezone,
          year: 'numeric',
          month: '2-digit',
          day: '2-digit',
        }).format(now);
        if (localDate !== utcDate) {
          return timezone;
        }
      } catch {
        // Ignore unsupported timezones and continue.
      }
    }
    return 'UTC';
  });
}

async function setTimezoneCookie(page: Page, timezone: string): Promise<void> {
  await page.context().setExtraHTTPHeaders({
    'X-Ovumcy-Timezone': timezone,
  });

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

async function timezoneToday(page: Page, timezone: string): Promise<{
  iso: string;
  day: string;
  weekdayEN: string;
  weekdayRU: string;
}> {
  return page.evaluate((tz) => {
    const now = new Date();
    const parts = new Intl.DateTimeFormat('en-CA', {
      timeZone: tz,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).formatToParts(now);

    const byType = Object.fromEntries(parts.map((part) => [part.type, part.value]));
    const iso = `${byType.year}-${byType.month}-${byType.day}`;
    return {
      iso,
      day: String(Number(byType.day)),
      weekdayEN: new Intl.DateTimeFormat('en-US', { timeZone: tz, weekday: 'long' }).format(now),
      weekdayRU: new Intl.DateTimeFormat('ru-RU', { timeZone: tz, weekday: 'long' }).format(now),
    };
  }, timezone);
}

test.describe('Bug regressions', () => {
  test.describe('BUG-01: request-local date consistency', () => {
    test('dashboard date subtitle, cycle day and calendar today badge use request timezone', async ({
      page,
    }) => {
      await page.goto('/login');
      const timezone = await pickTimezoneWithDifferentUTCDate(page);

      await registerOwnerAndReachDashboard(page, 'bug01-timezone');
      await setTimezoneCookie(page, timezone);

      const expectedToday = await timezoneToday(page, timezone);

      await page.goto('/settings');
      await expect(page).toHaveURL(/\/settings$/);

      // Remove onboarding-generated logs so cycle-day math is anchored only by the date
      // we set in cycle settings below.
      const csrfToken = (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
      const clearResponse = await page.request.post('/api/settings/clear-data', {
        form: { csrf_token: csrfToken },
        maxRedirects: 0,
      });
      expect([200, 303]).toContain(clearResponse.status());

      await page.goto('/settings');
      await expect(page).toHaveURL(/\/settings$/);

      const cycleForm = page.locator('section#settings-cycle form[action="/settings/cycle"]');
      await expect(cycleForm).toBeVisible();
      await fillDateField(
        cycleForm.locator('#settings-last-period-start'),
        shiftISODate(expectedToday.iso, -2)
      );
      await cycleForm.locator('button[data-save-button]').click();
      await expect(page.locator('#settings-cycle-status .status-ok')).toBeVisible();
      const savedStartISO = await cycleForm.locator('#settings-last-period-start').inputValue();

      await page.goto('/dashboard');
      await expect(page).toHaveURL(/\/dashboard$/);

      const todayAction = await page.locator('form[hx-post^="/api/days/"]').first().getAttribute('hx-post');
      expect(todayAction).toMatch(/^\/api\/days\/\d{4}-\d{2}-\d{2}$/);
      const actualTodayISO = String(todayAction || '').replace('/api/days/', '');

      const todayCard = page
        .locator('form[hx-post^="/api/days/"]')
        .first()
        .locator('xpath=ancestor::section[contains(@class,"journal-card")][1]');
      const subtitleText = String((await todayCard.locator('p.journal-muted').first().textContent()) || '');
      expect(subtitleText).toContain(expectedToday.day);
      expect(
        subtitleText.includes(expectedToday.weekdayEN) || subtitleText.toLowerCase().includes(expectedToday.weekdayRU)
      ).toBeTruthy();

      const cycleStatusItem = page.locator('.dashboard-status-line .dashboard-status-item').nth(1);
      const cycleValueText = String((await cycleStatusItem.textContent()) || '');
      const cycleDayMatch = cycleValueText.match(/\d+/);
      expect(cycleDayMatch, `Cannot parse cycle day from "${cycleValueText}"`).toBeTruthy();
      const expectedCycleDay = page.evaluate(({ todayISO, startISO }) => {
        const today = new Date(`${todayISO}T00:00:00`);
        const start = new Date(`${startISO}T00:00:00`);
        return Math.floor((today.getTime() - start.getTime()) / 86400000) + 1;
      }, {
        todayISO: actualTodayISO,
        startISO: savedStartISO,
      });
      expect(Number(cycleDayMatch![0])).toBe(await expectedCycleDay);

      await page.goto('/calendar');
      await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);
      const todayButton = page.locator('button[data-day]:has(.calendar-today-pill)').first();
      await expect(todayButton).toBeVisible();
      await expect(todayButton).toHaveAttribute('data-day', expectedToday.iso);

    });
  });

  test.describe('BUG-02: registration privacy and enumeration', () => {
    test('duplicate registration does not reveal account existence phrase or leak query params', async ({
      page,
    }) => {
      const creds = await registerOwnerAndReachDashboard(page, 'bug02-duplicate');

      await page.request.post('/api/auth/logout', {
        form: {
          csrf_token:
            (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '',
        },
        maxRedirects: 0,
      });

      await page.goto('/register');
      await page.locator('#register-email').fill(creds.email);
      await page.locator('#register-password').fill('ValidPass1');
      await page.locator('#register-confirm-password').fill('ValidPass1');
      await page.locator('form[action="/api/auth/register"] button[type="submit"]').click();

      await expect(page).toHaveURL(/\/register$/);
      const currentURL = page.url().toLowerCase();
      expect(currentURL).not.toContain('email=');
      expect(currentURL).not.toContain('error=');

      const bodyText = String((await page.locator('body').textContent()) || '').toLowerCase();
      expect(bodyText).not.toContain('already exists');
      expect(bodyText).not.toContain('already registered');
      expect(bodyText).not.toContain('already in use');
      expect(bodyText).not.toContain('уже существует');
    });

    test('registration validation errors do not leak email or error in URL', async ({ page }) => {
      await page.goto('/register');
      await page.locator('#register-email').fill('anyuser@ovumcy.lan');
      await page.locator('#register-password').fill('weak');
      await page.locator('#register-confirm-password').fill('weak');
      await page.locator('form[action="/api/auth/register"] button[type="submit"]').click();

      await expect(page).toHaveURL(/\/register$/);
      const currentURL = page.url().toLowerCase();
      expect(currentURL).not.toContain('email=');
      expect(currentURL).not.toContain('error=');
    });

    test('login unknown email and wrong password produce identical message', async ({ page }) => {
      const creds = await registerOwnerAndReachDashboard(page, 'bug02-login-generic');

      const csrf = (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
      await page.request.post('/api/auth/logout', {
        form: { csrf_token: csrf },
        maxRedirects: 0,
      });

      await page.goto('/login');
      await page.locator('#login-email').fill('doesnotexist@ovumcy.lan');
      await page.locator('#login-password').fill('SomePass1');
      await page.locator('form[action="/api/auth/login"] button[type="submit"]').click();
      await expect(page).toHaveURL(/\/login$/);
      const unknownMessage = String((await page.locator('.status-error').first().textContent()) || '').trim();

      await page.goto('/login');
      await page.locator('#login-email').fill(creds.email);
      await page.locator('#login-password').fill('WrongPass1');
      await page.locator('form[action="/api/auth/login"] button[type="submit"]').click();
      await expect(page).toHaveURL(/\/login$/);
      const wrongPasswordMessage = String((await page.locator('.status-error').first().textContent()) || '').trim();

      expect(unknownMessage).toBeTruthy();
      expect(wrongPasswordMessage).toBe(unknownMessage);
    });
  });

  test.describe('BUG-03: profile name immediate nav update', () => {
    test('settings profile save keeps the header identity empty and persists the field value', async ({ page }) => {
      await registerOwnerAndReachDashboard(page, 'bug03-profile-live');

      await page.goto('/settings');
      await expect(page).toHaveURL(/\/settings$/);

      const newName = `TestUser_${Date.now()}`;
      const nameInput = page.locator('#settings-display-name');
      await nameInput.fill(newName);

      await page.locator('form[action="/api/settings/profile"] button[data-save-button]').click();
      await expect(page.locator('#settings-profile-status .status-ok')).toBeVisible();

      await expect(page.locator('.nav-user-chip')).toHaveCount(0);
      await page.reload();
      await expect(page.locator('#settings-display-name')).toHaveValue(newName);
      await expect(page.locator('.nav-user-chip')).toHaveCount(0);
    });
  });
});
