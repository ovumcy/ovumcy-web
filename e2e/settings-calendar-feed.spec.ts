import { expect, test, type Locator, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  expectValueNotInWebStorage,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';

// Browser coverage for the read-only .ics feed lifecycle. The subscribe URL is
// the one sanctioned secret-in-URL carve-out (calendar clients send no cookie,
// so the path token is the auth), and its browser-visible contract is that the
// URL is WRITE-ONLY: it appears on the dedicated reveal page exactly once and is
// never rendered again — not on settings, not on a reload, not on a second visit
// to the reveal page. Backend regressions pin the server side; this spec pins
// what the owner's browser actually receives.
//
// The absence assertions below are anchored positively inside the same test: the
// very hook asserted missing on /settings ([data-calendar-feed-url]) is asserted
// PRESENT on the reveal page a few lines earlier, so a dead hook cannot make the
// negatives pass for the wrong reason.

const FEED_SECTION = '#settings-calendar-feed';
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

function feedSection(page: Page): Locator {
  return page.locator(FEED_SECTION);
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
    await expect(feedSection(page).locator('[data-calendar-feed-status]')).toHaveAttribute(
      'data-calendar-feed-status',
      'not-configured'
    );
    await expect(feedSection(page).locator('[data-settings-calendar-feed-rotate]')).toHaveCount(0);
    await expect(feedSection(page).locator('[data-settings-calendar-feed-revoke]')).toHaveCount(0);
    await expect(page.locator(FEED_URL_HOOK)).toHaveCount(0);

    const revealed = await generateFeed(page);

    // The secret never reaches client storage either (same guarantee the
    // recovery code carries).
    await expectValueNotInWebStorage(page, revealed.url);

    await leaveRevealPage(page);

    // Armed settings page: status flips, the lifecycle controls swap — and the
    // URL is gone from the DOM entirely.
    await expect(feedSection(page).locator('[data-calendar-feed-status]')).toHaveAttribute(
      'data-calendar-feed-status',
      'configured'
    );
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

    // Rotate and revoke carry `data-confirm`, but the dialog it opens does not
    // gate an htmx-driven form: the request is issued by the submit event
    // itself, so the click below is the action to bind the wait to. Once these
    // forms gate the request behind the dialog, this spec accepts it here.
    const rotateForm = feedSection(page).locator('[data-settings-calendar-feed-rotate]');
    await submitAndAwait(page, 'POST', '/api/v1/users/current/calendar-feed/rotate', () =>
      rotateForm.locator('button[type="submit"]').click()
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
    await submitAndAwait(page, 'DELETE', '/api/v1/users/current/calendar-feed', () =>
      revokeForm.locator('button[type="submit"]').click()
    );
    await expect(page.locator('#settings-calendar-feed-status .status-ok')).toBeVisible();

    await page.reload();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(feedSection(page).locator('[data-calendar-feed-status]')).toHaveAttribute(
      'data-calendar-feed-status',
      'not-configured'
    );
    await expect(feedSection(page).locator('[data-settings-calendar-feed-generate]')).toBeVisible();
    await expect(feedSection(page).locator('[data-settings-calendar-feed-rotate]')).toHaveCount(0);
    await expect(feedSection(page).locator('[data-settings-calendar-feed-revoke]')).toHaveCount(0);
    await expectNoSubscribeURLRendered(page, [first.token, rotated.token]);

    // With no feed armed there is nothing left to reveal.
    await page.goto('/settings/calendar-feed');
    await expect(page).toHaveURL(/\/settings$/);
    await expectNoSubscribeURLRendered(page, [first.token, rotated.token]);
  });
});
