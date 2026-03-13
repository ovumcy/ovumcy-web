import { expect, test, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  cookieByName,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  loginViaUI,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';

type EnvCredentials = {
  email: string;
  password: string;
};

function partnerCredentialsFromEnv(): EnvCredentials | null {
  const email = String(process.env.E2E_PARTNER_EMAIL || '').trim();
  const password = String(process.env.E2E_PARTNER_PASSWORD || '').trim();

  if (!email || !password) {
    return null;
  }

  return { email, password };
}

async function registerOwnerAndReachDashboard(page: Page, prefix: string): Promise<void> {
  const creds = createCredentials(prefix);

  await registerOwnerViaUI(page, creds);
  await expectInlineRegisterRecoveryStep(page);

  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);
}

async function registerOwnerAndOpenSettings(page: Page, prefix: string): Promise<void> {
  await registerOwnerAndReachDashboard(page, prefix);
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);
}

async function openTodayNotes(page: Page): Promise<void> {
  const disclosure = page.locator('details.note-disclosure');
  const isOpen = await disclosure.evaluate((element) => element.hasAttribute('open'));
  if (!isOpen) {
    await disclosure.locator('summary').click();
  }
  await expect(page.locator('#today-notes')).toBeVisible();
}

function todayISOInBrowser(): Promise<string> {
  return Promise.resolve().then(() => {
    const now = new Date();
    const yyyy = now.getFullYear();
    const mm = String(now.getMonth() + 1).padStart(2, '0');
    const dd = String(now.getDate()).padStart(2, '0');
    return `${yyyy}-${mm}-${dd}`;
  });
}

async function readCSRFToken(page: Page): Promise<string> {
  const csrfToken = await page.locator('meta[name="csrf-token"]').getAttribute('content');
  expect(csrfToken).toBeTruthy();
  return csrfToken ?? '';
}

test.describe('Security and role-based access', () => {
  test('xss in profile display name is escaped and never executes', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'security-xss-profile');

    let dialogTriggered = false;
    page.on('dialog', async (dialog) => {
      dialogTriggered = true;
      await dialog.dismiss();
    });

    const payload = `<img src=x onerror=alert('xss-profile')>`;
    await page.locator('#settings-display-name').fill(payload);
    await page.locator('form[action="/api/settings/profile"] button[data-save-button]').click();

    const primaryNavUserChip = page.locator('.nav-user-chip').first();

    await expect(page.locator('#settings-profile-status .status-ok')).toBeVisible();
    await expect(primaryNavUserChip).toContainText(payload);
    await expect(primaryNavUserChip.locator('img')).toHaveCount(0);
    await expect(page.locator('#settings-display-name')).toHaveValue(payload);
    await expect(page.locator('#settings-account img')).toHaveCount(0);

    await page.waitForTimeout(250);
    expect(dialogTriggered).toBe(false);
  });

  test('xss payload in notes is stored as plain text and does not execute', async ({ page }) => {
    await registerOwnerAndReachDashboard(page, 'security-xss-notes');

    let dialogTriggered = false;
    page.on('dialog', async (dialog) => {
      dialogTriggered = true;
      await dialog.dismiss();
    });

    const todayAction = await page.locator('form[hx-post^="/api/days/"]').first().getAttribute('hx-post');
    expect(todayAction).toMatch(/^\/api\/days\/\d{4}-\d{2}-\d{2}$/);
    const savedDay = String(todayAction || '').replace('/api/days/', '');

    const payload = `<script>alert('xss-notes')</script><img src=x onerror=alert('xss-notes-img')>`;
    await openTodayNotes(page);
    await page.locator('#today-notes').fill(payload);
    await page.locator('button[data-save-button]').first().click();
    await expect(page.locator('#save-status .status-ok')).toBeVisible();

    const month = savedDay.slice(0, 7);
    await page.goto(`/calendar?month=${month}&day=${savedDay}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${month}&day=${savedDay}`));
    await expect(page.locator('#day-editor')).toContainText(payload);

    await page.waitForTimeout(250);
    expect(dialogTriggered).toBe(false);
  });

  test('csrf basics: missing token is rejected for state-changing endpoints', async ({ page }) => {
    await registerOwnerAndReachDashboard(page, 'security-csrf');

    const logoutNoCsrf = await page.request.post('/api/auth/logout', {
      form: {},
      maxRedirects: 0,
    });
    expect(logoutNoCsrf.status()).toBe(403);

    const clearNoCsrf = await page.request.post('/api/settings/clear-data', {
      form: {},
      maxRedirects: 0,
    });
    expect(clearNoCsrf.status()).toBe(403);

    const exportNoCsrf = await page.request.post('/api/export/csv', {
      form: {},
      maxRedirects: 0,
    });
    expect(exportNoCsrf.status()).toBe(403);

    const csrfToken = await readCSRFToken(page);

    const clearWithCsrf = await page.request.post('/api/settings/clear-data', {
      form: { csrf_token: csrfToken },
      maxRedirects: 0,
    });
    expect([200, 303]).toContain(clearWithCsrf.status());
  });

  test('auth cookie keeps expected security flags', async ({ page, context }) => {
    await registerOwnerAndReachDashboard(page, 'security-cookie-flags');

    const authCookie = await cookieByName(context, 'ovumcy_auth');
    expect(authCookie).toBeTruthy();
    expect(authCookie?.httpOnly).toBe(true);
    expect(authCookie?.sameSite).toBe('Lax');

    const isHttps = page.url().startsWith('https://');
    expect(authCookie?.secure).toBe(isHttps);
  });

  test('owner can access owner-only sections and export', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'security-owner-access');

    await expect(page.locator('section#settings-cycle')).toBeVisible();
    await expect(page.locator('#settings-symptoms-section')).toBeVisible();
    await expect(page.locator('[data-export-section]')).toBeVisible();
    await expect(page.locator('form[action="/api/settings/clear-data"]')).toBeVisible();

    const exportResponse = await page.request.post('/api/export/csv', {
      form: { csrf_token: await readCSRFToken(page) },
    });
    expect(exportResponse.status()).toBe(200);
    expect(exportResponse.headers()['content-type'] || '').toContain('text/csv');
  });

  test('partner is read-only: owner-only settings and APIs are blocked', async ({ page }) => {
    const partnerCredentials = partnerCredentialsFromEnv();
    test.skip(
      !partnerCredentials,
      'Set E2E_PARTNER_EMAIL and E2E_PARTNER_PASSWORD to run partner role checks'
    );
    if (!partnerCredentials) {
      return;
    }

    await loginViaUI(page, partnerCredentials);
    await expect(page).toHaveURL(/\/dashboard$/);

    await expect(page.locator('form[hx-post^="/api/days/"]')).toHaveCount(0);
    await expect(page.locator('#today-notes')).toHaveCount(0);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    await expect(page.locator('section#settings-cycle')).toHaveCount(0);
    await expect(page.locator('#settings-symptoms-section')).toHaveCount(0);
    await expect(page.locator('[data-export-section]')).toHaveCount(0);
    await expect(page.locator('form[action="/api/settings/clear-data"]')).toHaveCount(0);

    const exportForbidden = await page.request.post('/api/export/csv', {
      form: { csrf_token: await readCSRFToken(page) },
      maxRedirects: 0,
    });
    expect(exportForbidden.status()).toBe(403);

    const csrfToken = await readCSRFToken(page);

    const todayISO = await todayISOInBrowser();
    const upsertForbidden = await page.request.post(`/api/days/${todayISO}`, {
      form: {
        csrf_token: csrfToken,
        is_period: 'true',
      },
      maxRedirects: 0,
    });
    expect(upsertForbidden.status()).toBe(403);
  });
});
