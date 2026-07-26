import { expect, test, type Locator, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';

// Browser coverage for the webhook reminder settings surface. The endpoint URL
// is write-only — it can embed an ntfy/Gotify token, so the settings page renders
// status plus at most the host, never the value — and a blank field means "keep
// the stored endpoint", which is why clearing it needs its own control.
//
// Delivery itself is a batch notify pass, not a request-triggered side effect, so
// nothing here asserts an outbound call: this spec covers only what the owner
// sees and submits.

const WEBHOOK_SECTION = '#settings-webhook';
const WEBHOOK_ENDPOINT = 'https://hooks.example.com/ovumcy/e2e-endpoint';
const WEBHOOK_HOST = 'hooks.example.com';

async function registerOwnerAndOpenSettings(page: Page, prefix: string): Promise<void> {
  const credentials = createCredentials(prefix);

  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);
}

function webhookSection(page: Page): Locator {
  return page.locator(WEBHOOK_SECTION);
}

function webhookURLField(page: Page): Locator {
  return webhookSection(page).locator('[data-settings-webhook-url]');
}

/**
 * Saves the webhook form and blocks until this click's own POST has committed.
 * Binding to the request rather than to any matching response keeps an earlier
 * in-flight request from satisfying the wait under load (the reasoning behind
 * saveDayEditorForm in calendar-autofill-clear.spec.ts).
 */
async function saveWebhookSettings(page: Page): Promise<void> {
  const [request] = await Promise.all([
    page.waitForRequest(
      (candidate) =>
        candidate.method() === 'POST' &&
        new URL(candidate.url()).pathname === '/api/v1/users/current/webhook'
    ),
    webhookSection(page).locator('[data-settings-webhook-save]').click(),
  ]);

  const response = await request.response();
  expect(response, 'expected a response for POST /api/v1/users/current/webhook').not.toBeNull();
  expect(
    response!.ok(),
    `POST /api/v1/users/current/webhook failed with ${response!.status()}`
  ).toBeTruthy();

  await expect(page.locator('#settings-webhook-status .status-ok')).toBeVisible();
}

async function reopenSettings(page: Page): Promise<void> {
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);
}

test.describe('Settings: webhook reminders', () => {
  test('enabling with a valid URL saves, renders the field blank, and the remove control clears it', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-webhook-lifecycle');

    const section = webhookSection(page);
    await expect(section).toBeVisible();

    // Unconfigured baseline: no endpoint, no remove affordance.
    await expect(section.locator('[data-webhook-status]')).toHaveAttribute(
      'data-webhook-status',
      'not-configured'
    );
    await expect(section.locator('[data-webhook-host]')).toHaveCount(0);
    await expect(section.locator('[data-webhook-setting="remove-url"]')).toHaveCount(0);
    await expect(webhookURLField(page)).toHaveValue('');

    await webhookURLField(page).fill(WEBHOOK_ENDPOINT);
    await section.locator('input[name="webhook_enabled"]').check();
    await section.locator('input[name="webhook_notify_period"]').check();
    await expect(section.locator('[data-webhook-setting="enabled"]')).toHaveAttribute(
      'data-active',
      'true'
    );
    await saveWebhookSettings(page);

    await reopenSettings(page);

    // Configured: the toggles persist and the host is shown — but the URL field
    // itself comes back BLANK. The stored endpoint is a secret the page must
    // never echo, so a blank field means "keep it", not "cleared".
    await expect(section.locator('[data-webhook-status]')).toHaveAttribute(
      'data-webhook-status',
      'configured'
    );
    await expect(section.locator('[data-webhook-host]')).toHaveText(WEBHOOK_HOST);
    await expect(section.locator('input[name="webhook_enabled"]')).toBeChecked();
    await expect(section.locator('input[name="webhook_notify_period"]')).toBeChecked();
    await expect(webhookURLField(page)).toHaveValue('');
    // Not just the input value: nothing on the page carries the full endpoint.
    expect(await page.content()).not.toContain(WEBHOOK_ENDPOINT);

    // Saving again with the field left blank keeps the endpoint rather than
    // wiping it — the reason a dedicated remove control has to exist at all.
    await saveWebhookSettings(page);
    await reopenSettings(page);
    await expect(section.locator('[data-webhook-status]')).toHaveAttribute(
      'data-webhook-status',
      'configured'
    );
    await expect(section.locator('[data-webhook-host]')).toHaveText(WEBHOOK_HOST);

    // The dedicated remove control is the only way to clear it, and it also
    // forces delivery off.
    const removeToggle = section.locator('[data-webhook-setting="remove-url"]');
    await expect(removeToggle).toBeVisible();
    await section.locator('input[name="webhook_remove_url"]').check();
    await saveWebhookSettings(page);

    await reopenSettings(page);
    await expect(section.locator('[data-webhook-status]')).toHaveAttribute(
      'data-webhook-status',
      'not-configured'
    );
    await expect(section.locator('[data-webhook-host]')).toHaveCount(0);
    await expect(section.locator('[data-webhook-setting="remove-url"]')).toHaveCount(0);
    await expect(section.locator('input[name="webhook_enabled"]')).not.toBeChecked();
    await expect(webhookURLField(page)).toHaveValue('');
  });

  test('enabling with a malformed URL is refused and leaves the webhook unconfigured', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-webhook-invalid-url');

    const section = webhookSection(page);
    // `novalidate` is not set on this form, so the browser would block a submit
    // of a type="url" field that fails its own parse. Use a value the browser
    // accepts as a URL but the service refuses: only http/https reach delivery.
    await webhookURLField(page).fill('ftp://hooks.example.com/ovumcy');
    await section.locator('input[name="webhook_enabled"]').check();
    await section.locator('[data-settings-webhook-save]').click();

    await expect(page.locator('#settings-webhook-status .status-error')).toBeVisible();

    await reopenSettings(page);
    await expect(section.locator('[data-webhook-status]')).toHaveAttribute(
      'data-webhook-status',
      'not-configured'
    );
    await expect(section.locator('input[name="webhook_enabled"]')).not.toBeChecked();
  });
});
