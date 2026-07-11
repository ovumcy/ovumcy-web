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

    await switchPublicLanguage(page, 'en');
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    await expect(page.locator('h1.journal-title')).toContainText('Log in to your account');

    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    await expect(page.locator('h1.journal-title')).toContainText('Log in to your account');

    await switchPublicLanguage(page, 'es');
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'es');
    await expect(page.locator('h1.journal-title')).toContainText('Inicia sesión en tu cuenta');

    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('lang', 'es');
    await expect(page.locator('h1.journal-title')).toContainText('Inicia sesión en tu cuenta');

    await switchPublicLanguage(page, 'ru');
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru');
    await expect(page.locator('h1.journal-title')).toContainText('Войти в аккаунт');
    await expect(page.locator('label[for="login-email"]')).toHaveText('Эл. почта');

    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru');
    await expect(page.locator('h1.journal-title')).toContainText('Войти в аккаунт');
    await expect(page.locator('label[for="login-email"]')).toHaveText('Эл. почта');

    await switchPublicLanguage(page, 'fr');
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'fr');
    await expect(page.locator('h1.journal-title')).toContainText('Connexion à votre compte');
    await expect(page.locator('label[for="login-email"]')).toHaveText('E-mail');

    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('lang', 'fr');
    await expect(page.locator('h1.journal-title')).toContainText('Connexion à votre compte');
    await expect(page.locator('label[for="login-email"]')).toHaveText('E-mail');

    await switchPublicLanguage(page, 'de');
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'de');
    await expect(page.locator('h1.journal-title')).toContainText('Melden Sie sich bei Ihrem Konto an');
    await expect(page.locator('label[for="login-email"]')).toHaveText('E-Mail');

    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('lang', 'de');
    await expect(page.locator('h1.journal-title')).toContainText('Melden Sie sich bei Ihrem Konto an');
    await expect(page.locator('label[for="login-email"]')).toHaveText('E-Mail');

    await switchPublicLanguage(page, 'it');
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'it');
    await expect(page.locator('h1.journal-title')).toContainText('Accedi al tuo account');
    await expect(page.locator('label[for="login-email"]')).toHaveText('Email');

    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('lang', 'it');
    await expect(page.locator('h1.journal-title')).toContainText('Accedi al tuo account');
    await expect(page.locator('label[for="login-email"]')).toHaveText('Email');
  });

  test('language switch while logged in keeps current page and translates navigation/settings', async ({
    page,
  }) => {
    await registerAndReachDashboard(page, 'nav-lang-auth');

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    await expect(page.locator('[data-settings-interface-form]')).toBeVisible();

    await saveSettingsLanguage(page, 'en');
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    await expect(page.locator('h1.journal-title')).toContainText('Settings');
    await expect(page.getByRole('link', { name: 'Today' }).first()).toBeVisible();

    await saveSettingsLanguage(page, 'es');
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'es');
    await expect(page.locator('h1.journal-title')).toContainText('Configuración');
    await expect(page.getByRole('link', { name: 'Hoy' }).first()).toBeVisible();
    await expectDateFieldVisible(page, 'settings-last-period-start');
    await expectDateFieldVisible(page, 'export-from');
    await expectDateFieldVisible(page, 'export-to');
    await expect(page.locator('[data-date-field-id="export-from"] [data-date-field-open]')).toBeVisible();
    await expect(page.locator('[data-date-field-id="export-to"] [data-date-field-open]')).toBeVisible();

    await saveSettingsLanguage(page, 'ru');
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru');
    await expect(page.locator('h1.journal-title')).toContainText('Настройки');
    await expect(page.getByRole('link', { name: 'Сегодня' }).first()).toBeVisible();

    await saveSettingsLanguage(page, 'fr');
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'fr');
    await expect(page.locator('h1.journal-title')).toContainText('Paramètres');
    await expect(page.getByRole('link', { name: "Aujourd'hui" }).first()).toBeVisible();
    await expectDateFieldVisible(page, 'settings-last-period-start');

    await saveSettingsLanguage(page, 'de');
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'de');
    await expect(page.locator('h1.journal-title')).toContainText('Einstellungen');
    await expect(page.getByRole('link', { name: 'Heute' }).first()).toBeVisible();
    await expectDateFieldVisible(page, 'settings-last-period-start');

    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('lang', 'de');
    await expect(page.locator('h1.journal-title')).toContainText('Einstellungen');

    await saveSettingsLanguage(page, 'it');
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'it');
    await expect(page.locator('h1.journal-title')).toContainText('Impostazioni');
    await expect(page.getByRole('link', { name: 'Oggi' }).first()).toBeVisible();
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
