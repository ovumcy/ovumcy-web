import { expect, type BrowserContext, type Locator, type Page } from '@playwright/test';
import { selectOnboardingStartDate } from './onboarding-helpers';

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
  await page.locator('#register-consent').check();
  await requestSubmitForm(page.locator('form[action="/api/v1/users"]'));
}

// registrationSettleTimeout covers the one step in the suite whose server cost
// is a deliberate expense rather than a query: POST /api/v1/users hashes a
// password and a recovery code with bcrypt before it can answer. Measured at
// 5.8-7.3s under parallel runs, which puts it past Playwright's 5s default and
// made this the suite's most frequent flake. The wait itself stays bound to a
// concrete signal — the landed URL and the rendered recovery block — so only
// the budget is widened, never the condition.
const registrationSettleTimeout = 20_000;

export async function expectInlineRegisterRecoveryStep(page: Page): Promise<void> {
  await expect(page).toHaveURL(/\/register(?:\?.*)?$/, { timeout: registrationSettleTimeout });
  await expect(page.locator('[data-auth-inline-recovery]')).toBeVisible({
    timeout: registrationSettleTimeout,
  });
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

  await page.locator('form[action="/api/v1/sessions"] button[type="submit"]').click();
}

export async function readRecoveryCode(page: Page): Promise<string> {
  const raw = (await page.locator('#recovery-code').textContent()) ?? '';
  const recoveryCode = raw.trim();
  expect(recoveryCode).toMatch(RECOVERY_CODE_PATTERN);
  return recoveryCode;
}

export async function confirmRecoveryCode(page: Page): Promise<void> {
  const form = page.locator('form[data-recovery-code-confirm]');
  const checkbox = form.locator('[data-recovery-code-checkbox]');
  const submit = form.locator('[data-recovery-code-submit]');

  await expect(form).toBeVisible();
  await checkbox.check();
  await expect(submit).toHaveAttribute('aria-disabled', 'false');
  await submit.click();
}

export async function continueFromRecoveryCode(page: Page): Promise<void> {
  await confirmRecoveryCode(page);
  await expect(page).toHaveURL(/\/(onboarding|dashboard)(?:\?.*)?$/);
}

const POST_REGISTRATION_PATHS = ['/onboarding', '/dashboard', '/login'];

export async function completeOnboardingIfPresent(page: Page): Promise<void> {
  const currentPath = pathOf(page.url());
  if (!POST_REGISTRATION_PATHS.includes(currentPath)) {
    // Tolerant of the redirect having already landed, never of it never landing:
    // the bare `.catch` used to swallow a hung redirect and let it resurface as
    // an unrelated failure further down whichever spec called this helper, so the
    // settled path is asserted here instead.
    await page
      .waitForURL((url) => POST_REGISTRATION_PATHS.includes(new URL(url).pathname), { timeout: 15000 })
      .catch(() => {});

    expect(
      POST_REGISTRATION_PATHS,
      `post-registration redirect never landed, still at ${page.url()}`
    ).toContain(pathOf(page.url()));
  }

  if (pathOf(page.url()) !== '/onboarding') {
    return;
  }

  const stepOneForm = page.locator('form[hx-post="/api/v1/onboarding/steps/1"]');
  const stepTwoForm = page.locator('form[hx-post="/api/v1/onboarding/steps/2"]');
  const isStepOneVisible = await stepOneForm.isVisible().catch(() => false);
  const isStepTwoVisible = await stepTwoForm.isVisible().catch(() => false);

  if (isStepOneVisible) {
    await selectOnboardingStartDate(page, isoDateDaysAgo(3));
    await stepOneForm.locator('button[type="submit"]').click();
  }

  if (!isStepOneVisible && !isStepTwoVisible) {
    throw new Error(`Unexpected onboarding state at ${page.url()}`);
  }

  await expect(stepTwoForm).toBeVisible();
  await Promise.all([
    page.waitForURL(/\/dashboard(?:\?.*)?$/, { timeout: 15000 }),
    // Step 2 carries two submit buttons — "Finish" and the skip action for the
    // mode question — so the finish control is addressed by its own hook.
    stepTwoForm.locator('[data-onboarding-step2-submit]').click(),
  ]);
}

/**
 * The one header a direct API call needs before the CSRF middleware will accept
 * it. Spread it into whatever headers the call already sends — the caller keeps
 * its own `X-CSRF-Token`, `Content-Type`, `HX-Request` and so on.
 *
 * Why it is needed at every `page.request.*` site: the middleware validates
 * `Origin` against the app-observed scheme+host on every mutating request, and an
 * API call sends none of its own — it is not a browser navigation. Plain HTTP has
 * nothing to compare and lets it through; HTTPS answers **403**. The harness
 * serves over TLS whenever `COOKIE_SECURE=true`, `E2E_USE_HTTPS_PROXY=true`, or
 * `E2E_OIDC_PROVIDER=local`, so a missing Origin turns into a pile of failures in
 * specs whose subject is something else entirely.
 */
export function apiOriginHeader(page: Page): Record<string, string> {
  return { Origin: new URL(page.url()).origin };
}

export async function logoutViaAPI(page: Page): Promise<void> {
  const csrfToken = await page.locator('meta[name="csrf-token"]').getAttribute('content');
  expect(csrfToken).toBeTruthy();

  // Origin is required over HTTPS — see apiOriginHeader above. This helper is the
  // one that made the gap visible: every spec that logs out through it failed the
  // moment the harness switched to TLS, which is why the suite had never run
  // against the HTTPS posture the public deployment profile recommends, and why
  // enabling the OIDC lane appeared to break thirty-odd unrelated tests.
  const response = await page.request.delete('/api/v1/sessions/current', {
    headers: apiOriginHeader(page),
    form: { csrf_token: csrfToken ?? '' },
    maxRedirects: 0,
  });

  expect([200, 303]).toContain(response.status());
}

export async function openForgotPasswordRecoveryStep(page: Page, email: string): Promise<void> {
  await page.goto('/forgot-password');
  await expect(page).toHaveURL(/\/forgot-password(?:\?.*)?$/);

  await page.locator('#forgot-email').fill(email);
  await page.locator('form[action="/api/v1/password-resets"] button[type="submit"]').click();

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
