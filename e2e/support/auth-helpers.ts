import { expect, type BrowserContext, type Locator, type Page } from '@playwright/test';
import { dateFieldRoot, fillDateField } from './date-field-helpers';

export type Credentials = {
  email: string;
  password: string;
};

const RECOVERY_CODE_PATTERN = /^OVUM-[A-Z0-9]{4}-[A-Z0-9]{4}-[A-Z0-9]{4}$/;

export const DEFAULT_STRONG_PASSWORD = 'StrongPass1';

export function createCredentials(prefix: string, password = DEFAULT_STRONG_PASSWORD): Credentials {
  const suffix = `${Date.now()}-${Math.floor(Math.random() * 1_000_000)}`;
  return {
    email: `${prefix}-${suffix}@example.com`,
    password,
  };
}

export function pathOf(urlString: string): string {
  return new URL(urlString).pathname;
}

export async function requestSubmitForm(form: Locator): Promise<void> {
  await form.evaluate((element) => {
    if (!(element instanceof HTMLFormElement)) {
      throw new Error('target is not an HTMLFormElement');
    }
    element.requestSubmit();
  });
}

export function expectNoSensitiveAuthParams(urlString: string): void {
  const url = new URL(urlString);
  const combined = `${url.search}${url.hash}`.toLowerCase();

  expect(combined).not.toContain('email=');
  expect(combined).not.toContain('error=');
  expect(combined).not.toContain('error_description=');
  expect(combined).not.toContain('code=');
  expect(combined).not.toContain('state=');
  expect(combined).not.toContain('iss=');
  expect(combined).not.toContain('token=');
  expect(combined).not.toContain('recovery=');
}

function isoDateDaysAgo(days: number): string {
  const date = new Date();
  date.setHours(0, 0, 0, 0);
  date.setDate(date.getDate() - days);

  const yyyy = date.getFullYear();
  const mm = String(date.getMonth() + 1).padStart(2, '0');
  const dd = String(date.getDate()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd}`;
}

export async function registerOwnerViaUI(
  page: Page,
  credentials: Credentials,
  confirmPassword = credentials.password
): Promise<void> {
  await page.goto('/register');
  await expect(page).toHaveURL(/\/register(?:\?.*)?$/);

  await page.locator('#register-email').fill(credentials.email);
  await page.locator('#register-password').fill(credentials.password);
  await page.locator('#register-confirm-password').fill(confirmPassword);
  await requestSubmitForm(page.locator('form[action="/api/auth/register"]'));
}

export async function expectInlineRegisterRecoveryStep(page: Page): Promise<void> {
  await expect(page).toHaveURL(/\/register(?:\?.*)?$/);
  await expect(page.locator('[data-auth-inline-recovery]')).toBeVisible();
}

export async function expectDedicatedRecoveryPage(page: Page): Promise<void> {
  await expect(page).toHaveURL(/\/recovery-code$/);
  await expect(page.locator('[data-recovery-code-tools]')).toBeVisible();
}

export async function loginViaUI(page: Page, credentials: Credentials, rememberMe = false): Promise<void> {
  await page.goto('/login');
  await expect(page).toHaveURL(/\/login(?:\?.*)?$/);

  await page.locator('#login-email').fill(credentials.email);
  await page.locator('#login-password').fill(credentials.password);

  if (rememberMe) {
    await page.locator('#login-remember-me').check();
  } else {
    await page.locator('#login-remember-me').uncheck();
  }

  await page.locator('form[action="/api/auth/login"] button[type="submit"]').click();
}

export async function readRecoveryCode(page: Page): Promise<string> {
  const raw = (await page.locator('#recovery-code').textContent()) ?? '';
  const recoveryCode = raw.trim();
  expect(recoveryCode).toMatch(RECOVERY_CODE_PATTERN);
  return recoveryCode;
}

export async function continueFromRecoveryCode(page: Page): Promise<void> {
  await page.locator('#recovery-code-saved').check();
  await page.locator('form[action] button[type="submit"]').click();
  await expect(page).toHaveURL(/\/(onboarding|dashboard)(?:\?.*)?$/);
}

export async function completeOnboardingIfPresent(page: Page): Promise<void> {
  const currentPath = pathOf(page.url());
  if (currentPath !== '/onboarding' && currentPath !== '/dashboard') {
    await page
      .waitForURL((url) => {
        const path = new URL(url).pathname;
        return path === '/onboarding' || path === '/dashboard' || path === '/login';
      })
      .catch(() => {});
  }

  if (pathOf(page.url()) !== '/onboarding') {
    return;
  }

  const startDateInput = page.locator('#last-period-start');
  const stepOneForm = page.locator('form[hx-post="/onboarding/step1"]');
  const stepTwoForm = page.locator('form[hx-post="/onboarding/step2"]');
  const isStepOneVisible = await stepOneForm.isVisible().catch(() => false);
  const isStepTwoVisible = await stepTwoForm.isVisible().catch(() => false);

  if (isStepOneVisible) {
    await expect(dateFieldRoot(startDateInput)).toBeVisible();
    await fillDateField(startDateInput, isoDateDaysAgo(3));
    await stepOneForm.locator('button[type="submit"]').click();
  }

  if (!isStepOneVisible && !isStepTwoVisible) {
    throw new Error(`Unexpected onboarding state at ${page.url()}`);
  }

  await expect(stepTwoForm).toBeVisible();
  await stepTwoForm.locator('button[type="submit"]').click();
  await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);
}

export async function logoutViaAPI(page: Page): Promise<void> {
  const csrfToken = await page.locator('meta[name="csrf-token"]').getAttribute('content');
  expect(csrfToken).toBeTruthy();

  const response = await page.request.post('/api/auth/logout', {
    form: { csrf_token: csrfToken ?? '' },
    maxRedirects: 0,
  });

  expect([200, 303]).toContain(response.status());
}

export async function openForgotPasswordRecoveryStep(page: Page, email: string): Promise<void> {
  await page.goto('/forgot-password');
  await expect(page).toHaveURL(/\/forgot-password(?:\?.*)?$/);

  await page.locator('#forgot-email').fill(email);
  await page.locator('form[action="/api/auth/forgot-password"] button[type="submit"]').click();

  await expect(page).toHaveURL(/\/forgot-password$/);
  await expect(page.locator('input[type="hidden"][name="email"]')).toHaveValue(email);
  await expect(page.locator('#recovery-code')).toBeVisible();
}

export async function cookieByName(context: BrowserContext, name: string) {
  const cookies = await context.cookies();
  return cookies.find((cookie) => cookie.name === name);
}

export async function enableClipboardRoundTripIfSupported(
  page: Page,
  context: BrowserContext
): Promise<boolean> {
  const origin = new URL(page.url()).origin;

  try {
    await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin });
  } catch {
    return false;
  }

  return page.evaluate(
    () =>
      typeof navigator.clipboard?.readText === 'function' &&
      typeof navigator.clipboard?.writeText === 'function'
  );
}

export async function expectValueNotInWebStorage(page: Page, secret: string): Promise<void> {
  const dump = await page.evaluate(() => {
    const local: Record<string, string> = {};
    const session: Record<string, string> = {};

    for (let i = 0; i < localStorage.length; i += 1) {
      const key = localStorage.key(i);
      if (key) {
        local[key] = localStorage.getItem(key) ?? '';
      }
    }

    for (let i = 0; i < sessionStorage.length; i += 1) {
      const key = sessionStorage.key(i);
      if (key) {
        session[key] = sessionStorage.getItem(key) ?? '';
      }
    }

    return { local, session };
  });

  expect(JSON.stringify(dump)).not.toContain(secret);
}
