import { expect, test, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  loginViaUI,
  logoutViaAPI,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { saveSettingsLanguage, switchPublicLanguage } from './support/language-helpers';
import { localeText, SUPPORTED_LOCALES, type Locale } from './support/locale-helpers';

// This spec's subject IS the localization, so rendered copy is asserted here on
// purpose — but every expected string comes from the catalogue the app ships
// (`internal/i18n/locales/*.json`), never re-typed per language. Elements are
// still addressed structurally (`[data-title-key]`, `[data-nav-link]`,
// `label[for=…]`), so a copy change syncs itself and a selector change fails
// loudly instead of silently matching nothing.
async function expectPageTitle(page: Page, locale: Locale, key: string): Promise<void> {
  const title = page.locator(`h1[data-title-key="${key}"]`);
  await expect(title).toBeVisible();
  await expect(title).toContainText(localeText(locale, key));
}

async function expectNavLink(page: Page, locale: Locale, hook: string, key: string): Promise<void> {
  const link = page.locator(`[data-nav-link="${hook}"]`).first();
  await expect(link).toHaveAttribute('data-nav-link-key', key);
  await expect(link).toHaveText(localeText(locale, key));
}

async function expectFieldLabel(
  page: Page,
  locale: Locale,
  fieldID: string,
  key: string
): Promise<void> {
  await expect(page.locator(`label[for="${fieldID}"]`)).toHaveText(localeText(locale, key));
}

async function registerAndReachDashboard(page: Page, prefix: string): Promise<{ email: string; password: string }> {
  const creds = createCredentials(prefix);

  await registerOwnerViaUI(page, creds);
  await expectInlineRegisterRecoveryStep(page);

  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);

  return creds;
}

async function expectDateFieldVisible(page: Page, fieldID: string): Promise<void> {
  const root = page.locator(`[data-date-field-id="${fieldID}"]`);
  await expect(root.locator('[data-date-field-part="day"]')).toBeVisible();
  await expect(root.locator('[data-date-field-part="month"]')).toBeVisible();
  await expect(root.locator('[data-date-field-part="year"]')).toBeVisible();
}

test.describe('Navigation and language switch', () => {
  test('unauthenticated user is redirected from protected routes to /login', async ({ page }) => {
    const protectedRoutes = ['/dashboard', '/calendar', '/stats', '/settings'];

    for (const route of protectedRoutes) {
      await page.goto(route);
      await expect(page).toHaveURL(/\/login$/);
    }
  });

  test('logo routes to /login when signed out and to /dashboard when signed in', async ({ page }) => {
    await page.goto('/login');
    await page.locator('a.brand-mark').click();
    await expect(page).toHaveURL(/\/login$/);

    await registerAndReachDashboard(page, 'nav-logo');
    await page.goto('/calendar');
    await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);

    await page.locator('a.brand-mark').click();
    await expect(page).toHaveURL(/\/dashboard$/);
  });

  test('public language switch on login page toggles EN/ES/RU/FR/DE/IT and persists after reload', async ({ page }) => {
    await page.goto('/login');
    await expect(page).toHaveURL(/\/login(?:\?.*)?$/);

    // Drive every supported locale from one list instead of a copy-pasted block
    // per language: a seventh locale is covered the day `SUPPORTED_LOCALES`
    // grows, and no expected string is typed into this file.
    for (const locale of SUPPORTED_LOCALES) {
      await switchPublicLanguage(page, locale);
      await expect(page).toHaveURL(/\/login$/);
      await expect(page.locator('html')).toHaveAttribute('lang', locale);
      await expectPageTitle(page, locale, 'auth.login_title');
      await expectFieldLabel(page, locale, 'login-email', 'auth.email');

      await page.reload();
      await expect(page.locator('html')).toHaveAttribute('lang', locale);
      await expectPageTitle(page, locale, 'auth.login_title');
      await expectFieldLabel(page, locale, 'login-email', 'auth.email');
    }
  });

  test('language switch while logged in keeps current page and translates navigation/settings', async ({
    page,
  }) => {
    await registerAndReachDashboard(page, 'nav-lang-auth');

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    await expect(page.locator('[data-settings-interface-form]')).toBeVisible();

    for (const locale of SUPPORTED_LOCALES) {
      await saveSettingsLanguage(page, locale);
      await expect(page).toHaveURL(/\/settings$/);
      await expect(page.locator('html')).toHaveAttribute('lang', locale);
      await expectPageTitle(page, locale, 'settings.title');
      await expectNavLink(page, locale, 'today', 'nav.today');

      // The segmented date fields must survive every locale — the browser-native
      // date control is deliberately not used, so a locale that broke the
      // segmented field would silently lose the whole control.
      await expectDateFieldVisible(page, 'settings-last-period-start');
      await expectDateFieldVisible(page, 'export-from');
      await expectDateFieldVisible(page, 'export-to');
      await expect(page.locator('[data-date-field-id="export-from"] [data-date-field-open]')).toBeVisible();
      await expect(page.locator('[data-date-field-id="export-to"] [data-date-field-open]')).toBeVisible();
    }

    // The saved preference survives a reload, not just the htmx swap that set it.
    const lastLocale = SUPPORTED_LOCALES[SUPPORTED_LOCALES.length - 1];
    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('lang', lastLocale);
    await expectPageTitle(page, lastLocale, 'settings.title');
  });

  test('direct /recovery-code access without valid recovery context is blocked', async ({ page }) => {
    await page.goto('/recovery-code');
    await expect(page).toHaveURL(/\/login$/);

    const creds = await registerAndReachDashboard(page, 'nav-recovery-guard');
    await logoutViaAPI(page);
    await loginViaUI(page, creds);
    await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);

    await page.goto('/recovery-code');

    await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);
    await expect(page.locator('#recovery-code')).toHaveCount(0);
  });
});
