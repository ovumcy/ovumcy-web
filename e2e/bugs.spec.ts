import { expect, test, type Page } from './support/fixtures';
import { fillDateField, formatDisplayDate } from './support/date-field-helpers';
import { selectOnboardingStartDate } from './support/onboarding-helpers';
import { openCalendarDayEditor } from './support/stats-helpers';
import { checkStyledControl } from './support/form-helpers';
import { saveSettingsLanguage } from './support/language-helpers';
import {
  dashboardCurrentCycleDay,
  dashboardCurrentPhase,
  dashboardStatusHeader,
  dashboardStatusLine,
  expectDashboardStatusHeader,
} from './support/dashboard-helpers';
import { everyLocaleText, localeKeysMatchingEnglish, localeText } from './support/locale-helpers';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  logoutViaAPI,
  readRecoveryCode,
  registerOwnerViaUI,
  apiOriginHeader,
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

async function onboardOwnerWithAutoPeriodFill(
  page: Page,
  prefix: string,
  onboardingDate: string,
  autoPeriodFill: boolean
): Promise<void> {
  await registerOwnerViaUI(page, createCredentials(prefix));
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);

  await selectOnboardingStartDate(page, onboardingDate);
  await page.locator('form[hx-post="/api/v1/onboarding/steps/1"] button[type="submit"]').click();
  await expect(page.locator('form[hx-post="/api/v1/onboarding/steps/2"]')).toBeVisible();

  const autoFillToggle = page.locator('label[data-binary-toggle]:has(input[name="auto_period_fill"])');
  const autoFillCheckbox = page.locator('input[name="auto_period_fill"]');
  // The toggle ships OFF for a new account, so this helper drives it to the
  // state its caller asked for rather than inheriting a default and flipping
  // once. The toggle label is clicked (not the input) so the binary-toggle
  // wiring the owner actually uses is what changes the value, and the
  // assertion below pins the resulting state either way.
  const initiallyChecked = await autoFillCheckbox.isChecked();
  if (initiallyChecked !== autoPeriodFill) {
    await autoFillToggle.click();
  }
  await expect(autoFillCheckbox).toBeChecked({ checked: autoPeriodFill });
  await expect(autoFillToggle).toHaveAttribute('data-active', String(autoPeriodFill));

  await page.locator('[data-onboarding-step2-submit]').click();
  await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);
}

async function expectAutoFillWindowMarkers(
  page: Page,
  onboardingDate: string,
  hasData: boolean
): Promise<void> {
  await page.goto(`/calendar?month=${onboardingDate.slice(0, 7)}&day=${onboardingDate}`);
  for (let offset = 0; offset < 5; offset += 1) {
    const iso = shiftISODate(onboardingDate, offset);
    await expect(page.locator(`button[data-day="${iso}"]`)).toHaveAttribute(
      'data-calendar-has-data',
      hasData ? 'true' : 'false'
    );
  }
}

async function setRangeValue(selector: string, page: Page, value: number): Promise<void> {
  await page.locator(selector).evaluate((element, rawValue) => {
    const input = element as HTMLInputElement;
    input.value = String(rawValue);
    input.dispatchEvent(new Event('input', { bubbles: true }));
    input.dispatchEvent(new Event('change', { bubbles: true }));
  }, value);
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
  weekday: string;
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
    // Derive the weekday in the language the page is actually rendering in,
    // rather than testing an EN-or-RU disjunction: that branch passed whenever
    // either language matched, so it could not notice a third language
    // rendering the wrong day.
    const lang = document.documentElement.lang || 'en';
    return {
      iso,
      day: String(Number(byType.day)),
      weekday: new Intl.DateTimeFormat(lang, { timeZone: tz, weekday: 'long' }).format(now),
    };
  }, timezone);
}

async function browserLocalISODate(page: Page): Promise<string> {
  return page.evaluate(() => {
    const now = new Date();
    const yyyy = now.getFullYear();
    const mm = String(now.getMonth() + 1).padStart(2, '0');
    const dd = String(now.getDate()).padStart(2, '0');
    return `${yyyy}-${mm}-${dd}`;
  });
}

async function browserMonthYearsAgo(page: Page, years: number): Promise<string> {
  return page.evaluate((offsetYears) => {
    const now = new Date();
    now.setFullYear(now.getFullYear() - offsetYears);
    const yyyy = now.getFullYear();
    const mm = String(now.getMonth() + 1).padStart(2, '0');
    return `${yyyy}-${mm}`;
  }, years);
}

const UNPREDICTABLE_EXPLAINER_KEY = 'prediction.explainer.unpredictable';

test.describe('Bug regressions', () => {
  test.describe('BUG-01: request-local date consistency', () => {
    test('dashboard date subtitle, cycle day and calendar today badge use request timezone', async ({
      page,
    }) => {
      await page.goto('/login');
      const timezone = await pickTimezoneWithDifferentUTCDate(page);

      const creds = await registerOwnerAndReachDashboard(page, 'bug01-timezone');
      await setTimezoneCookie(page, timezone);

      const expectedToday = await timezoneToday(page, timezone);

      await page.goto('/settings');
      await expect(page).toHaveURL(/\/settings$/);

      // Remove onboarding-generated logs so cycle-day math is anchored only by the date
      // we set in cycle settings below.
      const csrfToken = (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
      const clearResponse = await page.request.post('/api/v1/users/current/data-wipe', {
        headers: apiOriginHeader(page),
        form: {
          csrf_token: csrfToken,
          password: creds.password,
        },
        maxRedirects: 0,
      });
      expect([200, 303]).toContain(clearResponse.status());

      await page.goto('/settings');
      await expect(page).toHaveURL(/\/settings$/);

      const cycleForm = page.locator('#settings-cycle form[action="/api/v1/users/current/cycle"]');
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

      const todayAction = await page.locator('form[hx-put^="/api/v1/days/"]').first().getAttribute('hx-put');
      expect(todayAction).toMatch(/^\/api\/v1\/days\/\d{4}-\d{2}-\d{2}$/);
      const actualTodayISO = String(todayAction || '').replace('/api/v1/days/', '');

      // The date subtitle has its own hook now: `p.journal-muted` + `.first()`
      // picked whichever muted paragraph the card happened to render first.
      const subtitleText = String(
        (await page.locator('[data-dashboard-date-subtitle]').first().textContent()) || ''
      );
      expect(subtitleText).toContain(expectedToday.day);
      expect(
        subtitleText.toLowerCase(),
        `date subtitle "${subtitleText}" should name ${expectedToday.weekday}`
      ).toContain(expectedToday.weekday.toLowerCase());

      // The visible journal date carries the ISO day it edits, and the quick
      // switch offers the request-local yesterday — an evening entry made after
      // midnight has to be able to see and correct the day it lands on.
      await expect(page.locator('[data-dashboard-entry-date]').first()).toHaveAttribute(
        'data-dashboard-entry-date',
        actualTodayISO
      );
      await expect(
        page.locator('[data-dashboard-entry-date-switch] [data-entry-date-choice="yesterday"]')
      ).toHaveAttribute('data-entry-date', shiftISODate(actualTodayISO, -1));

      const expectedCycleDay = page.evaluate(({ todayISO, startISO }) => {
        const today = new Date(`${todayISO}T00:00:00`);
        const start = new Date(`${startISO}T00:00:00`);
        return Math.floor((today.getTime() - start.getTime()) / 86400000) + 1;
      }, {
        todayISO: actualTodayISO,
        startISO: savedStartISO,
      });
      expect(await dashboardCurrentCycleDay(page)).toBe(await expectedCycleDay);

      await page.goto('/calendar');
      await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);
      const todayButton = page.locator('button[data-day]:has(.calendar-today-pill)').first();
      await expect(todayButton).toBeVisible();
      await expect(todayButton).toHaveAttribute('data-day', expectedToday.iso);

    });

    test('calendar marks the current baseline period window, and withholds the fertile half of it, before manual day logs exist', async ({
      page,
    }) => {
      const creds = await registerOwnerAndReachDashboard(page, 'bug01-baseline-period');

      await page.goto('/settings');
      await expect(page).toHaveURL(/\/settings$/);

      const csrfToken = (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
      const clearResponse = await page.request.post('/api/v1/users/current/data-wipe', {
        headers: apiOriginHeader(page),
        form: {
          csrf_token: csrfToken,
          password: creds.password,
        },
        maxRedirects: 0,
      });
      expect([200, 303]).toContain(clearResponse.status());

      await page.goto('/settings');
      await expect(page).toHaveURL(/\/settings$/);

      const cycleForm = page.locator('#settings-cycle form[action="/api/v1/users/current/cycle"]');
      const todayISO = await browserLocalISODate(page);
      await fillDateField(cycleForm.locator('#settings-last-period-start'), shiftISODate(todayISO, -4));
      await setRangeValue('#settings-cycle-length', page, 28);
      await setRangeValue('#settings-period-length', page, 5);
      await cycleForm.locator('button[data-save-button]').click();
      await expect(page.locator('#settings-cycle-status .status-ok')).toBeVisible();

      const currentStart = await cycleForm.locator('#settings-last-period-start').inputValue();
      const currentDay = shiftISODate(currentStart, 4);
      const preFertileDay = shiftISODate(currentStart, 5);

      await page.goto(`/calendar?month=${currentDay.slice(0, 7)}&day=${currentDay}`);
      await expect(page.locator(`button[data-day="${currentDay}"]`)).toHaveClass(/calendar-cell-predicted/);
      // The account has recorded no cycle, so the fertile half of the projection
      // is withheld: the day after the baseline period window would be shaded
      // from the cycle-length slider alone, and a fertility claim with only a
      // configuration default behind it is suppressed rather than qualified. The
      // predicted period days asserted above stay — their anchor is the last
      // period start the owner entered, and only the length falls back.
      await expect(page.locator(`button[data-day="${preFertileDay}"]`)).toHaveAttribute('data-calendar-state', 'default');
    });

    test('onboarding with auto period fill disabled does not create logged-entry markers', async ({
      page,
    }) => {
      await page.goto('/login');

      // Pin onboardingDate to the 5th of a stable month so the +0..+4 window walked
      // below stays inside one calendar month — otherwise the loop crosses a month
      // boundary on early-month days and the rendered ?month=YYYY-MM grid has no
      // buttons for the spillover days. Falls back to the 5th of the prior month
      // when today's day-of-month is < 5 so the date stays in the past (onboarding
      // step1 rejects future dates).
      const todayISO = await browserLocalISODate(page);
      const [todayYear, todayMonth, todayDay] = todayISO.split('-').map((part) => Number(part));
      const monthAnchor =
        todayDay >= 5 ? new Date(todayYear, todayMonth - 1, 5) : new Date(todayYear, todayMonth - 2, 5);
      const onboardingDate = `${monthAnchor.getFullYear()}-${String(monthAnchor.getMonth() + 1).padStart(2, '0')}-05`;

      await onboardOwnerWithAutoPeriodFill(page, 'bug01-onboarding-no-autofill', onboardingDate, false);
      await expectAutoFillWindowMarkers(page, onboardingDate, false);

      // Positive anchor in the same test: a second owner onboarded with the
      // toggle left ON must mark the very same five cells. Without it the
      // "false" assertions above pass just as well when the marker attribute is
      // dead — an instance where nothing ever renders data-calendar-has-data
      // looks identical to auto-fill being correctly disabled. Owners are
      // isolated by user_id, so the second account observes only its own grid.
      await logoutViaAPI(page);
      await onboardOwnerWithAutoPeriodFill(page, 'bug01-onboarding-autofill', onboardingDate, true);
      await expectAutoFillWindowMarkers(page, onboardingDate, true);
    });

    test('dashboard status header next period stays aligned with calendar predicted start', async ({
      page,
    }) => {
      const creds = await registerOwnerAndReachDashboard(page, 'bug01-dashboard-calendar');

      await page.goto('/settings');
      await expect(page).toHaveURL(/\/settings$/);

      const csrfToken = (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
      const clearResponse = await page.request.post('/api/v1/users/current/data-wipe', {
        headers: apiOriginHeader(page),
        form: {
          csrf_token: csrfToken,
          password: creds.password,
        },
        maxRedirects: 0,
      });
      expect([200, 303]).toContain(clearResponse.status());

      await page.goto('/settings');
      await expect(page).toHaveURL(/\/settings$/);

      const cycleForm = page.locator('#settings-cycle form[action="/api/v1/users/current/cycle"]');
      const todayISO = await browserLocalISODate(page);
      const lastPeriodStart = shiftISODate(todayISO, -14);
      const nextPeriodStart = shiftISODate(lastPeriodStart, 28);
      const nextPeriodEnd = shiftISODate(lastPeriodStart, 32);

      await fillDateField(cycleForm.locator('#settings-last-period-start'), lastPeriodStart);
      await setRangeValue('#settings-cycle-length', page, 28);
      await setRangeValue('#settings-period-length', page, 5);
      await cycleForm.locator('button[data-save-button]').click();
      await expect(page.locator('#settings-cycle-status .status-ok')).toBeVisible();

      await page.goto('/dashboard');
      await expect(page).toHaveURL(/\/dashboard$/);

      const header = await expectDashboardStatusHeader(page);
      const nextPeriod = header.locator('[data-dashboard-next-period]');

      const expectedStartLabel = await formatDisplayDate(page, nextPeriodStart);
      const expectedEndLabel = await formatDisplayDate(page, nextPeriodEnd);
      await expect(nextPeriod).toContainText(`${expectedStartLabel} — ${expectedEndLabel}`);

      await page.goto(`/calendar?month=${nextPeriodStart.slice(0, 7)}&day=${nextPeriodStart}`);
      await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${nextPeriodStart.slice(0, 7)}&day=${nextPeriodStart}`));
      await expect(page.locator(`button[data-day="${nextPeriodStart}"]`)).toHaveClass(/calendar-cell-predicted/);
    });
  });

  test.describe('BUG-02: registration privacy and enumeration', () => {
    test('duplicate registration does not reveal account existence phrase or leak query params', async ({
      page,
    }) => {
      const creds = await registerOwnerAndReachDashboard(page, 'bug02-duplicate');

      await page.request.delete('/api/v1/sessions/current', {
        headers: apiOriginHeader(page),
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
      await page.locator('#register-consent').check();
      await page.locator('form[action="/api/v1/users"] button[type="submit"]').click();

      // Duplicate registration dispatches through the pickup-cookie flow:
      // POST /api/v1/users issues a decoy pickup cookie + 303 to
      // /register/welcome, the welcome handler fails to consume the decoy
      // nonce, and the browser ends at /login with a neutral flash. That
      // landing is the positive anchor — without it the URL and body checks
      // below stay green even if the decoy branch never ran at all. The flash
      // key is the one EVERY unusable pickup produces (decoy, expired,
      // tampered, replayed), so pinning it reveals nothing about whether the
      // address exists. The privacy invariant guarded here is that no URL
      // params leak the attempted email/error and no page text reveals
      // "already exists". The residual two-step landing-page oracle is
      // documented in SECURITY.md "Register enumeration".
      await expect(page).toHaveURL(/\/login$/);
      await expect(
        page.locator('[data-auth-server-error][data-error-key="auth.error.post_register_signin"]')
      ).toBeVisible();
      const currentURL = page.url().toLowerCase();
      expect(currentURL).not.toContain('email=');
      expect(currentURL).not.toContain('error=');

      // A privacy scan legitimately reads the whole body — the leak could be
      // anywhere on the page. What it must not do is enumerate phrases by hand
      // in two of six languages, which is what this checked before: a Spanish,
      // French, German or Italian leak walked straight past it.
      //
      // Derive instead. English is the catalogue's source language, so one
      // English phrase rule finds the KEYS that assert an account already
      // exists; `everyLocaleText` then expands those keys into every shipped
      // language, so a new locale is covered the day it lands.
      const existenceKeys = localeKeysMatchingEnglish(
        /already (exists|registered|in use|uses this email)/i
      );
      expect(
        existenceKeys.length,
        'the account-existence phrase rule must still match catalogue copy'
      ).toBeGreaterThan(0);
      const forbiddenPhrases = existenceKeys.flatMap((key) => everyLocaleText(key));

      const bodyText = String((await page.locator('body').textContent()) || '').toLowerCase();
      for (const phrase of forbiddenPhrases) {
        expect(bodyText, `body must not reveal account existence: "${phrase}"`).not.toContain(
          phrase.toLowerCase()
        );
      }
      // Same rule applied to the raw body, so an English string hardcoded
      // outside the catalogue cannot slip through either.
      expect(bodyText).not.toMatch(/already (exists|registered|in use|uses this email)/i);
    });

    test('registration validation errors do not leak email or error in URL', async ({ page }) => {
      await page.goto('/register');
      await page.locator('#register-email').fill('anyuser@ovumcy.lan');
      await page.locator('#register-password').fill('weak');
      await page.locator('#register-confirm-password').fill('weak');
      await page.locator('form[action="/api/v1/users"] button[type="submit"]').click();

      // Positive anchor: the weak password really was rejected. The client-side
      // validator blocks this submit before it reaches the network, so without
      // the visible-error assertion the URL checks below pass even when nothing
      // validated anything.
      await expect(page.locator('#register-client-status .status-error')).toBeVisible();
      await expect(page).toHaveURL(/\/register$/);
      const currentURL = page.url().toLowerCase();
      expect(currentURL).not.toContain('email=');
      expect(currentURL).not.toContain('error=');

      // The client validator swallows the UI submit above, so the server-side
      // error path — the surface this test is about — must be driven directly:
      // a weak password POSTed to /api/v1/users comes back as a redirect whose
      // Location must carry no email or error parameters.
      const csrfToken =
        (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
      const response = await page.request.post('/api/v1/users', {
        headers: apiOriginHeader(page),
        form: {
          csrf_token: csrfToken,
          email: 'anyuser@ovumcy.lan',
          password: 'weak',
          confirm_password: 'weak',
          consent: 'true',
        },
        maxRedirects: 0,
      });
      expect(response.status()).toBe(303);
      const location = String(response.headers()['location'] ?? '').toLowerCase();
      expect(location).not.toBe('');
      expect(location).not.toContain('email=');
      expect(location).not.toContain('error=');
    });

    test('login unknown email and wrong password produce identical message', async ({ page }) => {
      const creds = await registerOwnerAndReachDashboard(page, 'bug02-login-generic');

      const csrf = (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
      await page.request.delete('/api/v1/sessions/current', {
        headers: apiOriginHeader(page),
        form: { csrf_token: csrf },
        maxRedirects: 0,
      });

      await page.goto('/login');
      await page.locator('#login-email').fill('doesnotexist@ovumcy.lan');
      await page.locator('#login-password').fill('SomePass1');
      await page.locator('form[action="/api/v1/sessions"] button[type="submit"]').click();
      await expect(page).toHaveURL(/\/login$/);
      const unknownMessage = String((await page.locator('.status-error').first().textContent()) || '').trim();

      await page.goto('/login');
      await page.locator('#login-email').fill(creds.email);
      await page.locator('#login-password').fill('WrongPass1');
      await page.locator('form[action="/api/v1/sessions"] button[type="submit"]').click();
      await expect(page).toHaveURL(/\/login$/);
      const wrongPasswordMessage = String((await page.locator('.status-error').first().textContent()) || '').trim();

      expect(unknownMessage).toBeTruthy();
      expect(wrongPasswordMessage).toBe(unknownMessage);
    });
  });

  test.describe('BUG-03: profile name immediate nav update', () => {
    test('settings profile save updates the header identity without email fallback', async ({ page }) => {
      await registerOwnerAndReachDashboard(page, 'bug03-profile-live');

      await page.goto('/settings');
      await expect(page).toHaveURL(/\/settings$/);

      const identityChip = page.locator('#nav-user-chip-desktop');
      await expect(identityChip).toBeVisible();
      // Before a display name is saved the chip falls back to the profile-name
      // hint; take the expected string from the catalogue instead of pinning
      // the English wording here.
      await expect(identityChip).toHaveAttribute(
        'title',
        localeText('en', 'nav.profile_name_hint')
      );

      const newName = `TestUser_${Date.now()}`;
      const nameInput = page.locator('#settings-display-name');
      await nameInput.fill(newName);

      await page.locator('form[action="/api/v1/users/current/profile"] button[data-save-button]').click();
      await expect(page.locator('#settings-profile-status .status-ok')).toBeVisible();

      await expect(identityChip).toContainText(newName);
      const navOrderIsCorrect = await page.locator('[data-nav-account-actions]').evaluate((node) => {
        const identity = node.querySelector('#nav-user-chip-desktop');
        const logout = node.querySelector('.nav-logout-form');
        if (!identity || !logout) {
          return false;
        }
        return (identity.compareDocumentPosition(logout) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0;
      });
      expect(navOrderIsCorrect).toBeTruthy();
      await page.reload();
      await expect(page.locator('#settings-display-name')).toHaveValue(newName);
      const reloadedIdentityChip = page.locator('#nav-user-chip-desktop');
      await expect(reloadedIdentityChip).toContainText(newName);
      await expect(reloadedIdentityChip).not.toContainText('@');
    });
  });

  test.describe('BUG-04: unpredictable cycle mode and short-cycle UI', () => {
    test('unpredictable cycle hides dashboard predictions and suppresses the short-cycle warning in settings', async ({
      page,
    }) => {
      await registerOwnerAndReachDashboard(page, 'bug04-unpredictable');

      await page.goto('/settings');
      await expect(page).toHaveURL(/\/settings$/);

      const cycleForm = page.locator('#settings-cycle form[action="/api/v1/users/current/cycle"]');
      await expect(cycleForm).toBeVisible();

      await setRangeValue('#settings-cycle-length', page, 15);
      await setRangeValue('#settings-period-length', page, 5);
      await fillDateField(cycleForm.locator('#settings-last-period-start'), await browserLocalISODate(page));

      const shortCycleMessage = cycleForm.locator('[data-settings-cycle-message="cycle-short"]');
      await expect(shortCycleMessage).toBeVisible();

      await cycleForm.locator('input[name="unpredictable_cycle"]').check();
      await expect(shortCycleMessage).toBeHidden();

      await cycleForm.locator('button[data-save-button]').click();
      await expect(page.locator('#settings-cycle-status .status-ok')).toBeVisible();

      await page.goto('/dashboard');
      await expect(page).toHaveURL(/\/dashboard$/);

      // Unpredictable mode is a state the status header declares; assert it on
      // the hook and the phase attribute rather than on three copy fragments,
      // one of which ("Ovulation:") was a negated literal that any rewording
      // or language switch satisfies.
      const statusLine = dashboardStatusLine(page);
      await expect(statusLine).toBeVisible();
      await expect(dashboardStatusHeader(page)).toHaveAttribute('data-dashboard-phase', /.+/);
      // [data-dashboard-next-period] exists only in the predictions-on branch
      // of the status line — its absence is the
      // structural proof that predictions are off, which the old
      // `not.toContainText('Ovulation:')` only approximated in English.
      await expect(page.locator('[data-dashboard-next-period]')).toHaveCount(0);
      await expect(statusLine).not.toContainText(localeText('en', 'dashboard.ovulation'));
      await expect(statusLine).toContainText(localeText('en', 'dashboard.next_period_unknown'));
      await expect(statusLine).toContainText(
        localeText('en', 'prediction.explainer.unpredictable_compact')
      );

      await expect(page.locator('[data-dashboard-prediction-explainer]')).toHaveAttribute(
        'data-explainer-key',
        UNPREDICTABLE_EXPLAINER_KEY
      );

      await page.goto('/calendar');
      await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);
      const calendarExplainer = page.locator('[data-calendar-prediction-explainer]');
      await expect(calendarExplainer).toBeVisible();
      await expect(calendarExplainer).toHaveAttribute(
        'data-explainer-primary-key',
        UNPREDICTABLE_EXPLAINER_KEY
      );
      // One rendered-copy assertion for this state, from the catalogue.
      await expect(calendarExplainer).toContainText(localeText('en', UNPREDICTABLE_EXPLAINER_KEY));
    });
  });

  test.describe('BUG-05: calendar warning toast encoding', () => {
    test('Russian warning toast stays readable after saving a spotting day from calendar', async ({
      page,
    }) => {
      const creds = await registerOwnerAndReachDashboard(page, 'bug05-calendar-toast');

      await page.goto('/settings');
      await expect(page).toHaveURL(/\/settings$/);
      // Bind the language save to its own PATCH before the data-wipe API call
      // below — a bare save click races the in-flight request and can drop the
      // just-chosen language (saveSettingsLanguage documents the mechanism).
      await saveSettingsLanguage(page, 'ru');
      await expect(page.locator('html')).toHaveAttribute('lang', 'ru');

      const csrfToken = (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
      const clearResponse = await page.request.post('/api/v1/users/current/data-wipe', {
        headers: apiOriginHeader(page),
        form: {
          csrf_token: csrfToken,
          password: creds.password,
        },
        maxRedirects: 0,
      });
      expect([200, 303]).toContain(clearResponse.status());

      const todayISO = await browserLocalISODate(page);
      const dayEditorForm = await openCalendarDayEditor(page, todayISO);

      await dayEditorForm.locator('input[name="is_period"]').check();
      await checkStyledControl(dayEditorForm.locator('input[name="flow"][value="spotting"]'));
      await dayEditorForm.locator('button[data-save-button]').click();

      // The toast copy travels in the X-Ovumcy-Notice response header, which
      // carries no key — the client only ever sees the rendered sentence, so a
      // data-toast-key would mean widening that API surface. Source the
      // expected string from ru.json instead: this test is about the Russian
      // text surviving the URL-encoded round trip intact, so the exact
      // characters are the subject, and the catalogue owns them.
      await expect(page.locator('.toast-stack .toast-message').last()).toHaveText(
        localeText('ru', 'dashboard.spotting_cycle_warning')
      );
    });
  });

  test.describe('BUG-06: calendar backward navigation stays readable and bounded', () => {
    test('previous-month control keeps its label and becomes disabled at the lower bound', async ({
      page,
    }) => {
      await registerOwnerAndReachDashboard(page, 'bug06-calendar-prev');

      await page.goto('/calendar');
      await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);

      for (let index = 0; index < 6; index += 1) {
        const navActions = page
          .locator('section.space-y-6 > div.card')
          .first()
          .locator('.flex.flex-wrap.items-center.gap-2')
          .first();
        const previousControl = navActions.locator(':scope > *').first();

        await expect(previousControl).toContainText(/\S+/);
        const width = await previousControl.evaluate((node) => {
          return Math.round(node.getBoundingClientRect().width);
        });
        expect(width).toBeGreaterThan(44);

        const href = await previousControl.getAttribute('href');
        if (!href) {
          break;
        }

        // Name the month this iteration must land on: from the second iteration
        // on, the bare /calendar?month= shape is already satisfied by the URL the
        // click starts from, so the wait would not bind to the transition.
        const targetMonth = new URL(href, page.url()).searchParams.get('month');
        expect(targetMonth).toMatch(/^\d{4}-\d{2}$/);
        await previousControl.click();
        await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${targetMonth}$`));
      }

      const lowerBoundMonth = await browserMonthYearsAgo(page, 3);
      await page.goto(`/calendar?month=${lowerBoundMonth}`);
      await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${lowerBoundMonth}`));

      const navActions = page
        .locator('section.space-y-6 > div.card')
        .first()
        .locator('.flex.flex-wrap.items-center.gap-2')
        .first();
      const previousControl = navActions.locator(':scope > *').first();

      await expect(previousControl).toContainText(/\S+/);
      await expect(previousControl).toHaveClass(/btn-disabled/);
      await expect(previousControl).not.toHaveAttribute('href', /.+/);
    });
  });

  test.describe('IMPROVEMENTS: dashboard and stats polish', () => {
    test('dashboard menstrual phase stays clear in the primary summary', async ({ page }) => {
      await registerOwnerAndReachDashboard(page, 'improvement-menstrual-icon');

      // The phase is state, so assert the state attribute. The regex it
      // replaces listed the EN and ES spellings as separate alternatives of the
      // same word, which is what a per-language branch degenerates into.
      expect(await dashboardCurrentPhase(page)).toBe('menstrual');

      // The one header always renders the phase first, icon included, and the
      // cycle day sits inside the ring beside it. The icon is addressed by its
      // name in the first-party set: it is drawn markup, so it carries no text
      // for a copy assertion to find.
      const phaseChip = dashboardStatusLine(page).locator('.dashboard-status-item').first();
      await expect(phaseChip.locator('[data-icon="drop"]')).toHaveCount(1);
      await expect(phaseChip).toContainText(localeText('en', 'phases.menstrual'));
      expect(await dashboardCurrentCycleDay(page)).toBeGreaterThanOrEqual(1);
    });

    test('stats empty state includes illustration and progress affordance for a new owner', async ({
      page,
    }) => {
      await registerOwnerAndReachDashboard(page, 'improvement-stats-empty');

      await page.goto('/stats');
      await expect(page).toHaveURL(/\/stats$/);
      await expect(page.locator('.stats-empty-hero')).toBeVisible();
      await expect(page.locator('.stats-progress-meter')).toBeVisible();
    });
  });
});
