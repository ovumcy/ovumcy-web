import { expect, test, type Locator, type Page } from './support/fixtures';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import {
  acceptConfirmDialog,
  cancelConfirmDialog,
  mutatingRequestsDuring,
  submitAndAwait,
} from './support/confirm-dialog-helpers';

// Browser coverage for the webhook half of the egress ledger — the merged
// owner-only card that also carries the calendar feed (settings-calendar-feed.spec.ts
// covers that half). The endpoint URL is write-only — it can embed an ntfy/Gotify
// token, so the settings page renders status plus at most the host, never the
// value — and a blank field means "keep the stored endpoint", which is why
// clearing it needs its own control.
//
// The e2e instance runs with the reminder scheduler OFF (outboundDeliveryEnabled
// is false), so a saved, readable endpoint always renders the `outbound_disabled`
// state here, never `armed` — that state is out of reach in this environment
// regardless of the enabled/notify toggles. This spec pins the toggle values that
// persist underneath that state, not the (unreachable) armed rendering itself.
//
// Delivery itself is a batch notify pass, not a request-triggered side effect, so
// nothing here asserts an outbound call: this spec covers only what the owner
// sees and submits.

const EGRESS_CARD = '#settings-egress';
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

/** The webhook half of the merged card: `[data-egress-path="webhook"]`. */
function webhookPath(page: Page): Locator {
  return page.locator(EGRESS_CARD).locator('[data-egress-path="webhook"]');
}

function webhookURLField(page: Page): Locator {
  return webhookPath(page).locator('[data-settings-webhook-url]');
}

// `data-egress-webhook-state` lives on the same wrapper div `webhookPath`
// already selects (`[data-egress-path="webhook"]`), not on a descendant, so the
// state is asserted on that element directly rather than via a nested lookup.
async function expectWebhookState(page: Page, state: string): Promise<void> {
  await expect(webhookPath(page)).toHaveAttribute('data-egress-webhook-state', state);
}

/**
 * Saves the webhook form and blocks until this click's own POST has committed.
 * Binding to the request rather than to any matching response keeps an earlier
 * in-flight request from satisfying the wait under load (the reasoning behind
 * saveDayEditorForm in calendar-autofill-clear.spec.ts).
 *
 * The save form's `hx-target` is the whole `#settings-egress` card (it now
 * carries both paths), so the click's response replaces the entire card and
 * comes back open — the status island lives outside the swapped webhook/feed
 * halves, at `#settings-egress-status`.
 */
async function saveWebhookSettings(page: Page): Promise<void> {
  const [request] = await Promise.all([
    page.waitForRequest(
      (candidate) =>
        candidate.method() === 'POST' &&
        new URL(candidate.url()).pathname === '/api/v1/users/current/webhook'
    ),
    webhookPath(page).locator('[data-settings-webhook-save]').click(),
  ]);

  const response = await request.response();
  expect(response, 'expected a response for POST /api/v1/users/current/webhook').not.toBeNull();
  expect(
    response!.ok(),
    `POST /api/v1/users/current/webhook failed with ${response!.status()}`
  ).toBeTruthy();

  await expect(page.locator('#settings-egress-status .status-ok')).toBeVisible();
}

/**
 * Withdraws the stored endpoint through the dedicated remove form. It is
 * htmx-delete and gated by `hx-confirm` (replacing the old `remove-url`
 * checkbox that used to ride along with the save), so accepting the dialog —
 * not the submit click — is what releases the DELETE.
 */
async function removeWebhookEndpoint(page: Page): Promise<void> {
  const removeForm = webhookPath(page).locator('[data-settings-webhook-remove]');
  await expect(removeForm).toBeVisible();

  await submitAndAwait(page, 'DELETE', '/api/v1/users/current/webhook', async () => {
    await removeForm.locator('button[type="submit"]').click();
    await acceptConfirmDialog(page);
  });

  await expect(page.locator('#settings-egress-status .status-ok')).toBeVisible();
}

async function reopenSettings(page: Page): Promise<void> {
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);
}

test.describe('Settings: webhook reminders (egress ledger)', () => {
  test('enabling with a valid URL saves, renders the field blank, and the remove control clears it', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-webhook-lifecycle');

    const section = webhookPath(page);
    await expect(section).toBeVisible();

    // Unconfigured baseline: no endpoint was ever saved, so the state is
    // not_configured — distinct from outbound_disabled, which requires a
    // stored, readable endpoint to have been reached at all.
    await expectWebhookState(page, 'not_configured');
    await expect(section.locator('[data-egress-webhook-host]')).toHaveCount(0);
    await expect(section.locator('[data-settings-webhook-remove]')).toHaveCount(0);
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

    // Configured, but this instance runs with the reminder scheduler off, so a
    // stored+readable endpoint renders outbound_disabled rather than armed —
    // the state a working instance would show for the same stored values. The
    // toggles the owner set are what persist independently of that: they are
    // read straight back from storage.
    await expectWebhookState(page, 'outbound_disabled');
    await expect(section.locator('[data-egress-webhook-host]')).toHaveText(WEBHOOK_HOST);
    await expect(section.locator('input[name="webhook_enabled"]')).toBeChecked();
    await expect(section.locator('input[name="webhook_notify_period"]')).toBeChecked();
    await expect(webhookURLField(page)).toHaveValue('');
    // Not just the input value: nothing on the page carries the full endpoint.
    expect(await page.content()).not.toContain(WEBHOOK_ENDPOINT);

    // Saving again with the field left blank keeps the endpoint rather than
    // wiping it — the reason a dedicated remove control has to exist at all.
    await saveWebhookSettings(page);
    await reopenSettings(page);
    await expectWebhookState(page, 'outbound_disabled');
    await expect(section.locator('[data-egress-webhook-host]')).toHaveText(WEBHOOK_HOST);

    // The dedicated remove control (a separate hx-delete form, not a checkbox
    // on the save) is the only way to clear it, and it also forces delivery
    // off — the repository write behind DELETE /webhook sets webhook_enabled
    // back to false, even though it leaves the reminder-kind toggles alone.
    await removeWebhookEndpoint(page);

    await reopenSettings(page);
    await expectWebhookState(page, 'not_configured');
    await expect(section.locator('[data-egress-webhook-host]')).toHaveCount(0);
    await expect(section.locator('[data-settings-webhook-remove]')).toHaveCount(0);
    await expect(section.locator('input[name="webhook_enabled"]')).not.toBeChecked();
    await expect(webhookURLField(page)).toHaveValue('');
  });

  // Cancelling the confirmation must be inert. The sibling coverage for this
  // class of regression is the calendar feed's rotate/revoke tests in
  // settings-calendar-feed.spec.ts: both htmx-delete controls used to carry
  // `data-confirm`, which a document-level submit listener honors while htmx
  // itself issues the request from a listener on the form — so Cancel did not
  // stop the request. The remove-endpoint control is the same shape (hx-delete
  // gated by hx-confirm) and needs the same proof. The template-level guard
  // against the whole class is TestNoTemplateElementMixesHTMXRequestWithDataConfirm.
  test('cancelling the remove-endpoint confirmation leaves the stored endpoint working', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-webhook-remove-cancel');

    const section = webhookPath(page);
    await webhookURLField(page).fill(WEBHOOK_ENDPOINT);
    await section.locator('input[name="webhook_enabled"]').check();
    await saveWebhookSettings(page);
    await reopenSettings(page);
    await expectWebhookState(page, 'outbound_disabled');

    const removeForm = section.locator('[data-settings-webhook-remove]');
    await expect(removeForm).toBeVisible();

    const requestsDuringCancel = await mutatingRequestsDuring(
      page,
      (pathname) => pathname === '/api/v1/users/current/webhook',
      async () => {
        await removeForm.locator('button[type="submit"]').click();
        await cancelConfirmDialog(page);
        // A reload is the concrete signal that any request the click was
        // going to issue has had its chance — no arbitrary timeout involved.
        await page.reload();
        await expect(page).toHaveURL(/\/settings$/);
      }
    );
    expect(requestsDuringCancel, 'cancelling remove must issue no DELETE').toEqual([]);

    // Still stored — the assertion that would have caught the original
    // defect — and the host is still shown, so Cancel did not withdraw it
    // behind the owner's back.
    await expectWebhookState(page, 'outbound_disabled');
    await expect(section.locator('[data-egress-webhook-host]')).toHaveText(WEBHOOK_HOST);

    // Positive anchor: the same control, accepted, really does withdraw — so
    // the assertions above prove Cancel is inert, not that the control is dead.
    await removeWebhookEndpoint(page);
    await reopenSettings(page);
    await expectWebhookState(page, 'not_configured');
  });

  test('enabling with a malformed URL is refused and leaves the webhook unconfigured', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-webhook-invalid-url');

    const section = webhookPath(page);
    // `novalidate` is not set on this form, so the browser would block a submit
    // of a type="url" field that fails its own parse. Use a value the browser
    // accepts as a URL but the service refuses: only http/https reach delivery.
    await webhookURLField(page).fill('ftp://hooks.example.com/ovumcy');
    await section.locator('input[name="webhook_enabled"]').check();
    await section.locator('[data-settings-webhook-save]').click();

    await expect(page.locator('#settings-egress-status .status-error')).toBeVisible();

    await reopenSettings(page);
    await expectWebhookState(page, 'not_configured');
    await expect(section.locator('input[name="webhook_enabled"]')).not.toBeChecked();
  });
});
