import { expect, test, type Locator, type Page } from './support/fixtures';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { acceptConfirmDialog } from './support/confirm-dialog-helpers';
import {
  openCalendarDayEditor,
  registerAndOnboardWithStartDaysAgo,
  todayISOFromDashboard,
} from './support/stats-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';

// WEB-229. An in-page action that replaces, hides or disables the control the
// owner activated used to drop focus to BODY, so a keyboard or screen-reader
// owner restarted from the top of the document. Every site below is driven by
// the keyboard (focus the control, press Enter or Space — never a pointer) and
// asserts where `document.activeElement` lands afterwards: on the triggering
// control when it survives, on a stable target when it does not. One shared
// helper (`restoreFocus` in 40-shared-utils.js) serves them all.

async function activeElementSummary(page: Page): Promise<string> {
  return page.evaluate(() => {
    const node = document.activeElement;
    if (!node || node === document.body) {
      return 'BODY';
    }
    return `${node.tagName.toLowerCase()}${node.id ? `#${node.id}` : ''}`;
  });
}

async function expectFocusOn(page: Page, target: Locator, what: string): Promise<void> {
  try {
    await expect(target, what).toBeFocused({ timeout: 5000 });
  } catch (error) {
    throw new Error(`${what}: focus is on ${await activeElementSummary(page)}`, { cause: error });
  }
}

async function pressOn(target: Locator, key = 'Enter'): Promise<void> {
  await target.focus();
  await target.press(key);
}

async function registerOnboardedOwner(page: Page, prefix: string) {
  const credentials = createCredentials(prefix);
  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);
  await setRequestTimezoneFromBrowser(page);
  return credentials;
}

test.describe('Focus after in-page actions', () => {
  test('calendar day editor: Save keeps focus on the Save button', async ({ page }) => {
    await registerOnboardedOwner(page, 'focus-day-save');
    const today = await todayISOFromDashboard(page);

    const form = await openCalendarDayEditor(page, today);
    const save = form.locator('button[data-save-button]');
    await form
      .locator('label.choice-option:has(input[name="pregnancy_test"][value="negative"])')
      .click();

    const patch = page.waitForResponse(
      (response) =>
        response.request().method() === 'PATCH' && response.url().includes(`/api/v1/days/${today}`)
    );
    await pressOn(save);
    expect((await patch).ok()).toBeTruthy();

    await expect(page.locator('#calendar-save-status .status-ok')).toBeVisible();
    await expect(save).toBeEnabled();
    await expectFocusOn(page, save, 'Save');
  });

  test('calendar day editor: Remove result lands on the first result radio', async ({ page }) => {
    await registerOnboardedOwner(page, 'focus-remove-result');
    const today = await todayISOFromDashboard(page);

    const form = await openCalendarDayEditor(page, today);
    await form
      .locator('label.choice-option:has(input[name="pregnancy_test"][value="positive"])')
      .click();
    const patch = page.waitForResponse(
      (response) =>
        response.request().method() === 'PATCH' && response.url().includes(`/api/v1/days/${today}`)
    );
    await pressOn(form.locator('button[data-save-button]'));
    await patch;
    await expect(page.locator('#calendar-save-status .status-ok')).toBeVisible();

    const control = form.locator('[data-pregnancy-test]');
    const remove = control.locator('[data-pregnancy-test-remove]');
    await expect(remove).toBeVisible();
    await pressOn(remove);
    await expect(control).toHaveAttribute('data-pregnancy-test-state', 'absent');
    await expectFocusOn(
      page,
      control.locator('input[name="pregnancy_test"]').first(),
      `Remove result`
    );
  });

  test('late-cycle quick links move focus to the section they point at', async ({ page }) => {
    await registerAndOnboardWithStartDaysAgo(page, 'focus-late-cycle', 60);
    await page.goto('/dashboard');
    const actions = page.locator('[data-dashboard-late-cycle-actions]');
    await expect(actions).toBeVisible();

    for (const [action, targetID] of [
      ['cycle-start', 'dashboard-cycle-start'],
      ['pregnancy-test', 'dashboard-pregnancy-test'],
      ['symptoms', 'dashboard-symptoms'],
    ] as const) {
      const link = actions.locator(`[data-late-cycle-action="${action}"]`);
      await pressOn(link);
      await expect(page.locator(`#${targetID}`), action).toBeFocused();
    }
  });

  test('calendar feed: Revoke lands on the control the rebuilt card offers', async ({ page }) => {
    await registerOnboardedOwner(page, 'focus-feed-revoke');
    await page.goto('/settings');
    const feed = page.locator('#settings-egress [data-egress-path="calendar-feed"]');
    await feed.locator('[data-settings-calendar-feed-generate] button[type="submit"]').click();
    await page.waitForURL(/\/settings\/calendar-feed$/);
    await page.goto('/settings');

    const revoke = feed.locator('[data-settings-calendar-feed-revoke] button[type="submit"]');
    await pressOn(revoke);
    await acceptConfirmDialog(page);
    await expect(page.locator('#settings-egress-feed-status .status-ok')).toBeVisible();
    await expect(feed).toHaveAttribute('data-egress-feed-state', 'none');
    await expectFocusOn(
      page,
      feed.locator('[data-settings-calendar-feed-generate] button[type="submit"]'),
      `Revoke`
    );
  });

  test('goal quick switch: the reloaded dashboard puts focus back on the goal chip', async ({
    page,
  }) => {
    await registerOnboardedOwner(page, 'focus-goal-switch');
    await page.goto('/dashboard');

    const chip = page.locator('[data-usage-goal-chip]');
    await chip.focus();
    await chip.press('Enter');
    const choice = page.locator('[data-usage-goal-choice]').first();
    await expect(choice).toBeVisible();

    await Promise.all([
      page.waitForEvent('load'),
      pressOn(choice),
    ]);
    await expect(page.locator('[data-usage-goal-chip]')).toBeFocused();
  });

  test('cycle start: the reloaded page puts focus back on the cycle-start button', async ({
    page,
  }) => {
    await registerOnboardedOwner(page, 'focus-cycle-start');
    await page.goto('/dashboard');

    const button = page.locator('[data-dashboard-cycle-start-button]');
    await button.focus();
    const reloaded = page.waitForEvent('load');
    await button.press('Enter');
    // The confirm dialog only asks when the start would replace a recorded one.
    if (await page.locator('#confirm-modal').isVisible()) {
      await acceptConfirmDialog(page);
    }
    await reloaded;
    await expect(page.locator('[data-dashboard-cycle-start-button]')).toBeFocused();
  });

  test('cycle start in the calendar: the reloaded editor gets focus back on its button', async ({
    page,
  }) => {
    await registerOnboardedOwner(page, 'focus-cal-cycle-start');
    const today = await todayISOFromDashboard(page);
    const form = await openCalendarDayEditor(page, today);

    const button = page.locator('[data-day-cycle-start-button]');
    await expect(form).toBeVisible();
    await button.focus();
    const reloaded = page.waitForEvent('load');
    await button.press('Enter');
    if (await page.locator('#confirm-modal').isVisible()) {
      await acceptConfirmDialog(page);
    }
    await reloaded;
    await expect(page.locator('[data-day-cycle-start-button]')).toBeFocused();
  });
});
