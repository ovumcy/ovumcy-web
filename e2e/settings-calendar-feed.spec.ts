import { expect, test, type Locator, type Page } from './support/fixtures';
import {
  apiOriginHeader,
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  expectValueNotInWebStorage,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';

// Browser coverage for the read-only .ics feed lifecycle — the calendar half of
// the egress ledger, the merged owner-only card that also carries the webhook
// (settings-webhook.spec.ts covers that half). The subscribe URL is the one
// sanctioned secret-in-URL carve-out (calendar clients send no cookie, so the
// path token is the auth), and its browser-visible contract is that the URL is
// WRITE-ONLY: it appears on the dedicated reveal page exactly once and is never
// rendered again — not on settings, not on a reload, not on a second visit to
// the reveal page. Backend regressions pin the server side; this spec pins what
// the owner's browser actually receives. The reveal page itself is untouched by
// the settings-card merge, so its hooks (data-calendar-feed-reveal and friends)
// are unchanged below.
//
// The absence assertions below are anchored positively inside the same test: the
// very hook asserted missing on /settings ([data-calendar-feed-url]) is asserted
// PRESENT on the reveal page a few lines earlier, so a dead hook cannot make the
// negatives pass for the wrong reason.

const EGRESS_CARD = '#settings-egress';
const FEED_URL_HOOK = '[data-calendar-feed-url]';
const SUBSCRIBE_URL_PATTERN = /^https?:\/\/[^/]+\/calendar\/feed\/[^/]+\.ics$/;

type RevealedFeed = {
  url: string;
  token: string;
  heading: string;
};

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

/** The calendar-feed half of the merged card: `[data-egress-path="calendar-feed"]`. */
function feedSection(page: Page): Locator {
  return page.locator(EGRESS_CARD).locator('[data-egress-path="calendar-feed"]');
}

// `data-egress-feed-state` lives on the same wrapper div `feedSection` already
// selects (`[data-egress-path="calendar-feed"]`), not on a descendant, so the
// state is asserted on that element directly rather than via a nested lookup.
async function expectFeedState(page: Page, state: string): Promise<void> {
  await expect(feedSection(page)).toHaveAttribute('data-egress-feed-state', state);
}

/**
 * Bind the wait to the request this click issues rather than to any response for
 * the endpoint: `waitForResponse` can be satisfied by an earlier still-in-flight
 * request under load, while the `request` event only fires for requests issued
 * after registration (same reasoning as saveDayEditorForm in
 * calendar-autofill-clear.spec.ts). Awaiting that request's own response then
 * blocks until this mutation has truly committed.
 */
async function submitAndAwait(
  page: Page,
  method: 'POST' | 'DELETE',
  pathSuffix: string,
  action: () => Promise<void>
): Promise<void> {
  // The action is a callback, not a promise: Promise.all evaluates its elements
  // left to right, so the request waiter is registered before the click runs.
  const [request] = await Promise.all([
    page.waitForRequest(
      (candidate) =>
        candidate.method() === method && new URL(candidate.url()).pathname.endsWith(pathSuffix)
    ),
    action(),
  ]);

  const response = await request.response();
  expect(response, `expected a response for ${method} ${pathSuffix}`).not.toBeNull();
  expect(response!.ok(), `${method} ${pathSuffix} failed with ${response!.status()}`).toBeTruthy();
}

/** Reads the one-time reveal page and returns everything the caller may assert on. */
async function readRevealedFeed(page: Page): Promise<RevealedFeed> {
  await expect(page).toHaveURL(/\/settings\/calendar-feed$/);

  const revealCard = page.locator('[data-calendar-feed-reveal]');
  await expect(revealCard).toBeVisible();
  await expect(page.locator('[data-calendar-feed-reveal-warning]')).toBeVisible();
  // The feed ships predictions to a calendar app, so the estimate disclaimer
  // rides along with the link the owner is about to subscribe with.
  await expect(page.locator('[data-calendar-feed-disclaimer]')).toBeVisible();

  const urlBox = page.locator(FEED_URL_HOOK);
  await expect(urlBox).toBeVisible();
  const url = ((await urlBox.textContent()) ?? '').trim();
  expect(url).toMatch(SUBSCRIBE_URL_PATTERN);

  const heading = ((await page.locator('[data-calendar-feed-reveal-heading]').textContent()) ?? '').trim();
  expect(heading).not.toBe('');

  return { url, token: feedToken(url), heading };
}

function feedToken(subscribeURL: string): string {
  const token = subscribeURL.split('/calendar/feed/')[1]?.replace(/\.ics$/, '') ?? '';
  expect(token).not.toBe('');
  return token;
}

/** Generates the feed from a disarmed settings page and lands on the reveal page. */
async function generateFeed(page: Page): Promise<RevealedFeed> {
  const generateForm = feedSection(page).locator('[data-settings-calendar-feed-generate]');
  await expect(generateForm).toBeVisible();

  await submitAndAwait(page, 'POST', '/api/v1/users/current/calendar-feed', () =>
    generateForm.locator('button[type="submit"]').click()
  );
  await page.waitForURL(/\/settings\/calendar-feed$/);

  return readRevealedFeed(page);
}

/**
 * Rotate and revoke are htmx-driven and gated by `hx-confirm`, so htmx itself
 * withholds the request until the dialog resolves. Accepting is therefore what
 * issues it — binding the wait to the submit click would hang.
 */
async function acceptConfirmDialog(page: Page): Promise<void> {
  await expect(page.locator('#confirm-modal')).toBeVisible();
  await page.locator('#confirm-modal-accept').click();
}

async function cancelConfirmDialog(page: Page): Promise<void> {
  await expect(page.locator('#confirm-modal')).toBeVisible();
  await page.locator('#confirm-modal-cancel').click();
  await expect(page.locator('#confirm-modal')).toBeHidden();
}

/** Records every request the page issues to pathSuffix while `action` runs. */
async function requestsIssuedDuring(
  page: Page,
  pathSuffix: string,
  action: () => Promise<void>
): Promise<string[]> {
  const seen: string[] = [];
  const listener = (request: { url: () => string; method: () => string }) => {
    if (new URL(request.url()).pathname.endsWith(pathSuffix)) {
      seen.push(`${request.method()} ${request.url()}`);
    }
  };
  page.on('request', listener);
  try {
    await action();
  } finally {
    page.off('request', listener);
  }
  return seen;
}

async function leaveRevealPage(page: Page): Promise<void> {
  await page.locator('[data-calendar-feed-reveal-continue]').click();
  await page.waitForURL(/\/settings(?:\?.*)?$/);
}

/**
 * The core write-only assertion: no surface of the current page carries the
 * subscribe URL, neither through the reveal hook nor anywhere in the DOM.
 */
async function expectNoSubscribeURLRendered(page: Page, tokens: string[]): Promise<void> {
  await expect(page.locator(FEED_URL_HOOK)).toHaveCount(0);
  await expect(page.locator('[data-calendar-feed-reveal]')).toHaveCount(0);

  const markup = await page.content();
  for (const token of tokens) {
    expect(markup).not.toContain(token);
  }
}

test.describe('Settings: calendar feed one-time reveal', () => {
  test('generated subscribe URL is revealed once and never re-rendered afterwards', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'calendar-feed-reveal-once');

    // Disarmed baseline: only the generate affordance exists and no URL is shown.
    await expectFeedState(page, 'none');
    await expect(feedSection(page).locator('[data-settings-calendar-feed-rotate]')).toHaveCount(0);
    await expect(feedSection(page).locator('[data-settings-calendar-feed-revoke]')).toHaveCount(0);
    await expect(page.locator(FEED_URL_HOOK)).toHaveCount(0);

    const revealed = await generateFeed(page);

    // The secret never reaches client storage either (same guarantee the
    // recovery code carries).
    await expectValueNotInWebStorage(page, revealed.url);

    await leaveRevealPage(page);

    // Armed settings page: status flips, the lifecycle controls swap — and the
    // URL is gone from the DOM entirely. A freshly generated link is signed
    // under the key this instance uses now, hence issued_current_key.
    await expectFeedState(page, 'issued_current_key');
    await expect(feedSection(page).locator('[data-settings-calendar-feed-generate]')).toHaveCount(0);
    await expect(feedSection(page).locator('[data-settings-calendar-feed-rotate]')).toBeVisible();
    await expect(feedSection(page).locator('[data-settings-calendar-feed-revoke]')).toBeVisible();
    await expectNoSubscribeURLRendered(page, [revealed.token]);

    // A reload does not resurrect it.
    await page.reload();
    await expect(page).toHaveURL(/\/settings$/);
    await expectNoSubscribeURLRendered(page, [revealed.token]);

    // Neither does going back to the reveal page: the one-time cookie is spent,
    // so the page bounces to /settings with nothing revealed.
    await page.goto('/settings/calendar-feed');
    await expect(page).toHaveURL(/\/settings$/);
    await expectNoSubscribeURLRendered(page, [revealed.token]);
  });

  test('rotate reveals a different URL once and revoke returns the feed to disarmed', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'calendar-feed-rotate-revoke');

    const first = await generateFeed(page);
    await leaveRevealPage(page);

    // Rotate is gated by the confirm dialog, so accepting it — not the submit
    // click — is what issues the request.
    const rotateForm = feedSection(page).locator('[data-settings-calendar-feed-rotate]');
    await rotateForm.locator('button[type="submit"]').click();
    await submitAndAwait(page, 'POST', '/api/v1/users/current/calendar-feed/rotate', () =>
      acceptConfirmDialog(page)
    );
    await page.waitForURL(/\/settings\/calendar-feed$/);

    const rotated = await readRevealedFeed(page);
    expect(rotated.url).not.toBe(first.url);
    expect(rotated.token).not.toBe(first.token);
    // Rotation is a distinct state, so the reveal page says something different
    // than it did on the initial generate.
    expect(rotated.heading).not.toBe(first.heading);
    await expectValueNotInWebStorage(page, rotated.url);

    await leaveRevealPage(page);
    // Shown once applies to the rotated URL exactly as it did to the first, and
    // the superseded one stays gone too.
    await expectNoSubscribeURLRendered(page, [first.token, rotated.token]);

    const revokeForm = feedSection(page).locator('[data-settings-calendar-feed-revoke]');
    await revokeForm.locator('button[type="submit"]').click();
    await submitAndAwait(page, 'DELETE', '/api/v1/users/current/calendar-feed', () =>
      acceptConfirmDialog(page)
    );
    await expect(page.locator('#settings-egress-status .status-ok')).toBeVisible();

    // The response to the revoke IS the rebuilt card, so the new state must be
    // readable before any reload. Asserting only after a reload would stay green
    // if the mutation went back to answering with a bare status fragment, which
    // is what left a stale sentence standing beside a fresh success message.
    await expectFeedState(page, 'none');

    await page.reload();
    await expect(page).toHaveURL(/\/settings$/);
    await expectFeedState(page, 'none');
    await expect(feedSection(page).locator('[data-settings-calendar-feed-generate]')).toBeVisible();
    await expect(feedSection(page).locator('[data-settings-calendar-feed-rotate]')).toHaveCount(0);
    await expect(feedSection(page).locator('[data-settings-calendar-feed-revoke]')).toHaveCount(0);
    await expectNoSubscribeURLRendered(page, [first.token, rotated.token]);

    // With no feed armed there is nothing left to reveal.
    await page.goto('/settings/calendar-feed');
    await expect(page).toHaveURL(/\/settings$/);
    await expectNoSubscribeURLRendered(page, [first.token, rotated.token]);
  });

  // Cancelling a confirmation must be inert. This is a real regression, not a
  // hypothetical: rotate and revoke used to carry `data-confirm`, which is
  // honored by a document-level submit listener, while htmx listens on the form
  // itself and issues the request first — so the dialog was decorative and
  // Cancel rotated the feed anyway, invalidating a subscribe URL the owner had
  // just decided to keep. Both controls now use `hx-confirm`, which htmx gates
  // on. The template-level guard against the whole class is
  // TestNoTemplateElementMixesHTMXRequestWithDataConfirm.
  test('cancelling rotate or revoke leaves the armed feed working', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'calendar-feed-confirm-cancel');

    const armed = await generateFeed(page);
    await leaveRevealPage(page);

    // The subscribe URL works before any of this — the anchor every assertion
    // below is measured against.
    const beforeCancel = await page.request.get(armed.url, { headers: apiOriginHeader(page) });
    expect(beforeCancel.status(), 'the freshly generated feed must serve').toBe(200);

    const rotateForm = feedSection(page).locator('[data-settings-calendar-feed-rotate]');
    const rotateRequests = await requestsIssuedDuring(
      page,
      '/api/v1/users/current/calendar-feed/rotate',
      async () => {
        await rotateForm.locator('button[type="submit"]').click();
        await cancelConfirmDialog(page);
        // A reload is the concrete signal that any request the click was going
        // to issue has had its chance — no arbitrary timeout involved.
        await page.reload();
        await expect(page).toHaveURL(/\/settings$/);
      }
    );
    expect(rotateRequests, 'cancelling rotate must issue no request').toEqual([]);

    const revokeForm = feedSection(page).locator('[data-settings-calendar-feed-revoke]');
    const revokeRequests = await requestsIssuedDuring(
      page,
      '/api/v1/users/current/calendar-feed',
      async () => {
        await revokeForm.locator('button[type="submit"]').click();
        await cancelConfirmDialog(page);
        await page.reload();
        await expect(page).toHaveURL(/\/settings$/);
      }
    );
    expect(revokeRequests, 'cancelling revoke must issue no request').toEqual([]);

    // Still armed, and — the assertion that would have caught the original
    // defect — the ORIGINAL subscribe URL still serves, so neither cancelled
    // action rotated or revoked it behind the owner's back.
    await expectFeedState(page, 'issued_current_key');
    const afterCancel = await page.request.get(armed.url, { headers: apiOriginHeader(page) });
    expect(afterCancel.status(), 'a cancelled rotate/revoke must leave the URL working').toBe(200);

    // Positive anchor: the same control, accepted, really does revoke — so the
    // assertions above prove Cancel is inert, not that the control is dead.
    await revokeForm.locator('button[type="submit"]').click();
    await submitAndAwait(page, 'DELETE', '/api/v1/users/current/calendar-feed', () =>
      acceptConfirmDialog(page)
    );
    await expect(page.locator('#settings-egress-status .status-ok')).toBeVisible();

    const afterAccept = await page.request.get(armed.url, { headers: apiOriginHeader(page) });
    expect(afterAccept.status(), 'an accepted revoke must kill the URL').toBe(404);
  });
});
