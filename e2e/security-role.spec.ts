import { expect, test, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  cookieByName,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
  apiOriginHeader,
} from './support/auth-helpers';
import { ensureNotesFieldVisible } from './support/note-helpers';

async function registerOwnerAndReachDashboard(page: Page, prefix: string) {
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

async function registerOwnerAndOpenSettings(page: Page, prefix: string): Promise<void> {
  await registerOwnerAndReachDashboard(page, prefix);
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);
}

async function openTodayNotes(page: Page): Promise<void> {
  await ensureNotesFieldVisible(page, '#today-notes');
}

async function readCSRFToken(page: Page): Promise<string> {
  const csrfToken = await page.locator('meta[name="csrf-token"]').getAttribute('content');
  expect(csrfToken).toBeTruthy();
  return csrfToken ?? '';
}

/**
 * The mapped error envelope is an app-wide contract, so a refusal is pinned by
 * its stable key rather than by rendered copy. A status-only assertion stayed
 * green while the server answered these with the framework's bare "Forbidden"
 * string, which no client can branch on.
 */
type MappedErrorEnvelope = { error?: string; error_detail?: { key?: string } };

async function expectMappedErrorEnvelope(
  response: { text(): Promise<string> },
  expectedKey: string,
): Promise<void> {
  const body = await response.text();
  let payload: MappedErrorEnvelope;
  try {
    payload = JSON.parse(body) as MappedErrorEnvelope;
  } catch {
    throw new Error(`expected the mapped error envelope, got a non-JSON body: ${body}`);
  }
  expect(payload.error).toBe(expectedKey);
  expect(payload.error_detail?.key).toBe(expectedKey);
}

test.describe('Security and role-based access', () => {
  test('xss in profile display name is rejected and never executes', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'security-xss-profile');

    let dialogTriggered = false;
    page.on('dialog', async (dialog) => {
      dialogTriggered = true;
      await dialog.dismiss();
    });

    const payload = `<img src=x onerror=alert('xss-profile')>`;
    await page.locator('#settings-display-name').fill(payload);
    await page.locator('form[action="/api/v1/users/current/profile"] button[data-save-button]').click();

    const primaryNavUserChip = page.locator('[data-nav-account-actions] #nav-user-chip-desktop');

    await expect(page.locator('#settings-profile-status .status-error')).toBeVisible();
    await expect(page.locator('#settings-profile-status .status-ok')).toHaveCount(0);
    await expect(primaryNavUserChip).not.toContainText('xss-profile');
    await expect(primaryNavUserChip.locator('img')).toHaveCount(0);
    await expect(page.locator('#settings-display-name')).toHaveValue(payload);
    await expect(page.locator('#settings-account img')).toHaveCount(0);

    // The listener has been installed since before the save, and the assertions
    // above are the concrete signal that the payload has had its chance: the
    // server rejected it, and the surfaces that would have carried it hold no
    // `<img>` to fire a deferred `onerror`. No sleep is needed to make that safe.
    expect(dialogTriggered, 'the rejected display name must not execute').toBe(false);
  });

  test('xss payload in notes is stored as plain text and does not execute', async ({ page }) => {
    await registerOwnerAndReachDashboard(page, 'security-xss-notes');

    let dialogTriggered = false;
    page.on('dialog', async (dialog) => {
      dialogTriggered = true;
      await dialog.dismiss();
    });

    const todayAction = await page.locator('form[hx-put^="/api/v1/days/"]').first().getAttribute('hx-put');
    expect(todayAction).toMatch(/^\/api\/v1\/days\/\d{4}-\d{2}-\d{2}$/);
    const savedDay = String(todayAction || '').replace('/api/v1/days/', '');

    const payload = `<script>alert('xss-notes')</script><img src=x onerror=alert('xss-notes-img')>`;
    await openTodayNotes(page);
    await page.locator('#today-notes').fill(payload);
    await page.locator('button[data-save-button]').first().click();
    await expect(page.locator('#save-status .status-ok')).toBeVisible();

    const month = savedDay.slice(0, 7);
    await page.goto(`/calendar?month=${month}&day=${savedDay}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${month}&day=${savedDay}`));
    await expect(page.locator('#day-editor')).toContainText(payload);

    // The listener has been installed since before the save, and the assertion
    // above is the concrete signal: the payload is present as *text*, so neither
    // the `<script>` (which would have run during parsing) nor the `<img>` (which
    // would be an element, not text, and would fire `onerror` on its failed load)
    // was ever parsed as markup. No sleep is needed to make that safe.
    expect(dialogTriggered, 'the stored payload must not execute').toBe(false);
  });

  test('csrf basics: missing token is rejected for state-changing endpoints', async ({ page }) => {
    const creds = await registerOwnerAndReachDashboard(page, 'security-csrf');

    // Both calls carry a valid Origin and NO csrf_token, so the 403 below can only
    // come from the missing token — the point of this test. Without the header the
    // middleware refuses these over HTTPS for the missing Origin instead, and the
    // assertion passes while proving nothing about CSRF.
    const logoutNoCsrf = await page.request.delete('/api/v1/sessions/current', {
      headers: apiOriginHeader(page),
      form: {},
      maxRedirects: 0,
    });
    expect(logoutNoCsrf.status()).toBe(403);
    await expectMappedErrorEnvelope(logoutNoCsrf, 'forbidden');

    const clearNoCsrf = await page.request.post('/api/v1/users/current/data-wipe', {
      headers: apiOriginHeader(page),
      form: {},
      maxRedirects: 0,
    });
    expect(clearNoCsrf.status()).toBe(403);
    await expectMappedErrorEnvelope(clearNoCsrf, 'forbidden');

    // /api/v1/exports/* is GET-only on the v1 surface; CSRF only gates
    // state-changing methods, so an export without CSRF returns 200, not 403.

    const csrfToken = await readCSRFToken(page);

    const clearWithCsrf = await page.request.post('/api/v1/users/current/data-wipe', {
      headers: apiOriginHeader(page),
      form: {
        csrf_token: csrfToken,
        password: creds.password,
      },
      maxRedirects: 0,
    });
    expect([200, 303]).toContain(clearWithCsrf.status());
  });

  test('auth cookie keeps expected security flags', async ({ page, context }) => {
    await registerOwnerAndReachDashboard(page, 'security-cookie-flags');

    const authCookie = await cookieByName(context, 'ovumcy_auth');
    expect(authCookie).toBeTruthy();
    expect(authCookie?.httpOnly).toBe(true);

    const isHttps = page.url().startsWith('https://');
    expect(authCookie?.secure).toBe(isHttps);
  });

  test('owner can access owner-only sections and export', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'security-owner-access');

    await expect(page.locator('section#settings-cycle')).toBeVisible();
    await expect(page.locator('#settings-symptoms-section')).toBeVisible();
    await expect(page.locator('[data-export-section]')).toBeVisible();
    await expect(page.locator('form[action="/api/v1/users/current/data-wipe"]')).toBeVisible();

    // GET-only route: CSRF gates state-changing methods (see the comment in the
    // csrf-basics test above), so no token is sent; the explicit Origin keeps
    // the call valid under the HTTPS posture.
    const exportResponse = await page.request.get('/api/v1/exports/csv', {
      headers: apiOriginHeader(page),
    });
    expect(exportResponse.status()).toBe(200);
    expect(exportResponse.headers()['content-type'] || '').toContain('text/csv');
  });

});
