import { expect, test, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  loginViaUI,
  logoutViaAPI,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';

async function registerAndReachDashboard(
  page: Page,
  prefix: string
): Promise<{ email: string; password: string }> {
  const creds = createCredentials(prefix);

  await registerOwnerViaUI(page, creds);
  await expect(page).toHaveURL(/\/recovery-code$/);

  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);

  return creds;
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

  test('language switch on login page toggles EN/ES/RU and persists after reload', async ({ page }) => {
    await page.goto('/login');
    await expect(page).toHaveURL(/\/login(?:\?.*)?$/);

    await page.locator('.lang-switch a[href^="/lang/en"]').click();
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    await expect(page.locator('h1.journal-title')).toContainText('Log in to your account');
    await expect(page.locator('.lang-switch a[aria-current="page"]')).toHaveText('EN');

    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    await expect(page.locator('h1.journal-title')).toContainText('Log in to your account');

    await page.locator('.lang-switch a[href^="/lang/es"]').click();
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'es');
    await expect(page.locator('h1.journal-title')).toContainText('Inicia sesión en tu cuenta');
    await expect(page.locator('.lang-switch a[aria-current="page"]')).toHaveText('ES');

    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('lang', 'es');
    await expect(page.locator('h1.journal-title')).toContainText('Inicia sesión en tu cuenta');

    await page.locator('.lang-switch a[href^="/lang/ru"]').click();
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru');
    await expect(page.locator('h1.journal-title')).toContainText('Войти в аккаунт');
    await expect(page.locator('.lang-switch a[aria-current="page"]')).toHaveText('RU');

    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru');
    await expect(page.locator('h1.journal-title')).toContainText('Войти в аккаунт');
  });

  test('language switch while logged in keeps current page and translates navigation/settings', async ({
    page,
  }) => {
    await registerAndReachDashboard(page, 'nav-lang-auth');

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    await page.locator('.lang-switch a[href^="/lang/en"]').click();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'en');
    await expect(page.locator('h1.journal-title')).toContainText('Settings');
    await expect(page.getByRole('link', { name: 'Dashboard' }).first()).toBeVisible();

    await page.locator('.lang-switch a[href^="/lang/es"]').click();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'es');
    await expect(page.locator('h1.journal-title')).toContainText('Configuración');
    await expect(page.getByRole('link', { name: 'Panel' }).first()).toBeVisible();
    await expect(page.getByRole('textbox', { name: 'Día' })).toHaveCount(3);
    await expect(page.getByRole('textbox', { name: 'Mes' })).toHaveCount(3);
    await expect(page.getByRole('textbox', { name: 'Año' })).toHaveCount(3);
    await expect(page.getByRole('button', { name: 'Mostrar selector de fecha' })).toHaveCount(2);

    await page.locator('.lang-switch a[href^="/lang/ru"]').click();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru');
    await expect(page.locator('h1.journal-title')).toContainText('Настройки');
    await expect(page.getByRole('link', { name: 'Панель' }).first()).toBeVisible();

    await page.reload();
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru');
    await expect(page.locator('h1.journal-title')).toContainText('Настройки');
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
