import { expect, test, type Locator, type Page } from '@playwright/test';
import { mutatingRequestsDuring } from './support/confirm-dialog-helpers';
import { fillDateField } from './support/date-field-helpers';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { applyTheme, expectTextContrastAA } from './support/contrast-helpers';
import {
  assertNoHorizontalOverflow,
  expectElementAboveMobileTabbar,
  expectOpaqueMobileTabbar,
  expectPageBottomClearsMobileTabbar,
  expectVisibleFocusIndicator,
} from './support/mobile-layout-helpers';
import {
  markCycleStart,
  openCalendarDayEditor,
  registerOwnerAndEnableIrregularMode,
  saveBBTOnDay,
  saveCycleFactorOnDay,
  saveDayEditorForm,
  shiftISODate,
  todayISOFromDashboard,
} from './support/stats-helpers';

async function registerOwnerAndReachDashboard(page: Page, prefix: string): Promise<void> {
  const credentials = createCredentials(prefix);

  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);
}

async function setCurrentCycleStart(page: Page, isoDate: string): Promise<void> {
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);

  const cycleForm = page.locator('section#settings-cycle form[action="/api/v1/users/current/cycle"]');
  await expect(cycleForm).toBeVisible();
  await fillDateField(cycleForm.locator('#settings-last-period-start'), isoDate);
  await cycleForm.locator('button[data-save-button]').click();
  await expect(page.locator('#settings-cycle-status .status-ok')).toBeVisible();
}

async function seedStatsInsightState(page: Page, prefix: string): Promise<void> {
  await registerOwnerAndEnableIrregularMode(page, prefix);

  const today = await todayISOFromDashboard(page);
  const cycleStarts = [-112, -84, -56, -28].map((offset) => shiftISODate(today, offset));

  for (const cycleStart of cycleStarts) {
    await markCycleStart(page, cycleStart);
  }

  await saveCycleFactorOnDay(page, shiftISODate(cycleStarts[0], 2), 'stress');
  await saveCycleFactorOnDay(page, shiftISODate(cycleStarts[1], 2), 'travel');
  await saveCycleFactorOnDay(page, shiftISODate(cycleStarts[2], 2), 'stress');

  const currentCycleStart = shiftISODate(today, -8);
  await setCurrentCycleStart(page, currentCycleStart);

  const bbtDays = [0, 1, 2, 3, 4].map((offset) => shiftISODate(currentCycleStart, offset));
  const bbtValues = ['36.40', '36.45', '36.50', '36.55', '36.60'];
  for (let index = 0; index < bbtDays.length; index += 1) {
    await saveBBTOnDay(page, bbtDays[index], bbtValues[index]);
  }
}

test.describe('Visual and accessibility regressions', () => {
  test('mobile dashboard, settings, and privacy stay within the viewport and above the tabbar', async ({
    page,
  }) => {
    await registerOwnerAndReachDashboard(page, 'visual-mobile-layout');
    await page.setViewportSize({ width: 390, height: 844 });

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await assertNoHorizontalOverflow(page);
    // The autosave row renders nothing while idle — the journal is autosave-only
    // and says so only when it has something to report — so the lowest control
    // of the day form is the anchor for the tabbar clearance.
    const dashboardLowestAction = page.locator('[data-dashboard-cycle-start-button]');
    await dashboardLowestAction.scrollIntoViewIfNeeded();
    await expectElementAboveMobileTabbar(page, dashboardLowestAction);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await assertNoHorizontalOverflow(page);
    const trackingSave = page.locator('[data-settings-tracking-save]');
    await trackingSave.scrollIntoViewIfNeeded();
    await expectElementAboveMobileTabbar(page, trackingSave);

    await page.goto('/privacy?back=%2Fsettings');
    await expect(page).toHaveURL(/\/privacy\?back=%2Fsettings$/);
    await assertNoHorizontalOverflow(page);
    const sourceLink = page.locator('a[href="https://github.com/ovumcy/ovumcy-web"]');
    await sourceLink.scrollIntoViewIfNeeded();
    await expectElementAboveMobileTabbar(page, sourceLink);
  });

  test('mobile tabbar paints opaquely in both themes and page bottoms clear it', async ({
    page,
  }) => {
    await registerOwnerAndReachDashboard(page, 'visual-tabbar-opacity');
    await page.setViewportSize({ width: 390, height: 844 });

    const html = page.locator('html');
    const footerPrivacyLink = page.locator('footer a[href^="/privacy"]');

    for (const theme of ['light', 'dark'] as const) {
      await page.evaluate((value) => {
        window.localStorage.setItem('ovumcy_theme', value);
      }, theme);

      for (const path of ['/dashboard', '/calendar', '/stats']) {
        await page.goto(path);
        await expect(page).toHaveURL(new RegExp(`${path}$`));
        await expect(html).toHaveAttribute('data-theme', theme);

        await expectOpaqueMobileTabbar(page);
        await expectPageBottomClearsMobileTabbar(page, footerPrivacyLink);
      }
    }
  });

  test('primary navigation and actions show visible focus indicators', async ({
    page,
  }) => {
    await registerOwnerAndReachDashboard(page, 'visual-focus');
    await page.setViewportSize({ width: 1280, height: 900 });

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    const brandMark = page.locator('a.brand-mark');
    const todayLink = page.locator('nav.sm\\:flex a[href="/dashboard"]').first();
    const logoutButton = page.locator('.nav-logout-form button[type="submit"]').first();

    await brandMark.focus();
    await expect(brandMark).toBeFocused();
    await expectVisibleFocusIndicator(brandMark);

    await todayLink.focus();
    await expect(todayLink).toBeFocused();
    await expectVisibleFocusIndicator(todayLink);

    await logoutButton.focus();
    await expect(logoutButton).toBeFocused();
    await expectVisibleFocusIndicator(logoutButton);
  });

  test('skip-to-content link appears on focus and moves focus into main content', async ({
    page,
  }) => {
    await registerOwnerAndReachDashboard(page, 'visual-skip-link');
    await page.setViewportSize({ width: 1280, height: 900 });

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    const skipLink = page.locator('a.skip-link');
    // Visually parked off-screen until focused — this is CSS behavior only a
    // real browser can verify (the jsdom unit suite cannot see it).
    await expect(skipLink).toHaveCSS('position', 'absolute');
    const hiddenBox = await skipLink.boundingBox();
    expect(hiddenBox === null || hiddenBox.y < 0).toBeTruthy();

    // First Tab from a fresh page lands on the skip link and reveals it.
    await page.keyboard.press('Tab');
    await expect(skipLink).toBeFocused();
    const visibleBox = await skipLink.boundingBox();
    expect(visibleBox).not.toBeNull();
    expect(visibleBox!.y).toBeGreaterThanOrEqual(0);

    // Activating it moves focus into the main landmark, past the header.
    await page.keyboard.press('Enter');
    await expect(page.locator('#main-content')).toBeFocused();
  });

  test('logout confirm dialog traps Tab and restores focus on dismiss', async ({
    page,
  }) => {
    await registerOwnerAndReachDashboard(page, 'visual-focus-trap');
    await page.setViewportSize({ width: 1280, height: 900 });

    await page.goto('/dashboard');
    const logoutButton = page.locator('.nav-logout-form button[type="submit"]').first();
    await logoutButton.click();

    const modal = page.locator('#confirm-modal');
    await expect(modal).toBeVisible();
    await expect(page.locator('#confirm-modal-cancel')).toBeFocused();

    // Native Tab order must cycle inside the dialog: cancel -> accept ->
    // back to cancel, never into the page behind the backdrop.
    await page.keyboard.press('Tab');
    await expect(page.locator('#confirm-modal-accept')).toBeFocused();
    await page.keyboard.press('Tab');
    await expect(page.locator('#confirm-modal-cancel')).toBeFocused();
    await page.keyboard.press('Shift+Tab');
    await expect(page.locator('#confirm-modal-accept')).toBeFocused();

    // Escape closes the dialog, returns focus to the invoking button, and must
    // not release the logout it gated. A URL assertion is already true the moment
    // it runs, so record what the page puts on the wire across the whole window
    // and close it on a reload: a surviving session still serves /dashboard,
    // whereas an escaped logout would redirect to /login.
    const escapedLogouts = await mutatingRequestsDuring(
      page,
      (pathname) => pathname === '/logout',
      async () => {
        await page.keyboard.press('Escape');
        await expect(modal).toBeHidden();
        await expect(logoutButton).toBeFocused();

        await page.reload();
        await expect(page).toHaveURL(/\/dashboard$/);
      }
    );
    expect(escapedLogouts, 'dismissing the logout dialog must issue no logout').toEqual([]);
  });

  test('stats insight state stays readable on mobile and exposes accessible summaries', async ({
    page,
  }) => {
    test.slow();

    await seedStatsInsightState(page, 'visual-stats-mobile');
    await page.setViewportSize({ width: 390, height: 844 });

    await page.goto('/stats');
    await expect(page).toHaveURL(/\/stats$/);
    await assertNoHorizontalOverflow(page);
    await expect(page.locator('[data-stats-factor-context]')).toBeVisible();
    await expect(page.locator('#cycle-chart')).toBeVisible();
    await expect(page.locator('#cycle-chart')).toHaveAttribute('role', 'img');
    await expect(page.locator('#stats-cycle-trend-summary')).toBeVisible();

    // Unconditional: the seed saves five BBT readings inside the current cycle,
    // so HasCurrentCycleBBTChart is true and the summary the chart's
    // aria-describedby points at must be there. Guarding it on count > 0 made a
    // missing panel indistinguishable from a rendered one.
    await expect(page.locator('#stats-bbt-summary')).toBeVisible();

    const cycleSummary = page.locator('#stats-cycle-trend-summary');
    await cycleSummary.scrollIntoViewIfNeeded();
    await expectElementAboveMobileTabbar(page, cycleSummary);
  });

  test('mobile tap targets meet the minimum size (tabbar 44px, language pills 40px)', async ({
    page,
  }) => {
    await page.setViewportSize({ width: 390, height: 844 });

    // Pre-auth language pills are auth-free — check them before registering.
    await page.goto('/login');
    const pills = page.locator('.lang-switch .lang-link');
    const pillCount = await pills.count();
    expect(pillCount).toBeGreaterThan(0);
    const pillRowTops = new Set<number>();
    for (let index = 0; index < pillCount; index++) {
      const box = await pills.nth(index).boundingBox();
      expect(box, `language pill ${index} must have a visible box`).not.toBeNull();
      expect(box!.height, 'language pill must be at least 40px tall').toBeGreaterThanOrEqual(40);
      pillRowTops.add(Math.round(box!.y));
    }
    // Enlarging the tap area must not break the single-row layout at 390px.
    expect(pillRowTops.size, 'language pills must stay on a single row at 390px').toBe(1);
    await assertNoHorizontalOverflow(page);

    // Owner bottom tabbar links must fill the visible bar (>=44px effective).
    await registerOwnerAndReachDashboard(page, 'visual-tap-target');
    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/dashboard');
    const tabbarLinks = page.locator('nav.mobile-tabbar a');
    const linkCount = await tabbarLinks.count();
    expect(linkCount).toBeGreaterThan(0);
    for (let index = 0; index < linkCount; index++) {
      const box = await tabbarLinks.nth(index).boundingBox();
      expect(box, `tabbar link ${index} must have a visible box`).not.toBeNull();
      expect(box!.height, 'tabbar link must be at least 44px tall').toBeGreaterThanOrEqual(44);
    }
  });

  test('primary actions clear WCAG AA text contrast in both themes', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 });

    // Pre-auth first: the login submit is the same `.btn-primary` component that
    // carries every owner-facing primary action, and it needs no account.
    await page.goto('/login');
    await expect(page).toHaveURL(/\/login$/);

    for (const theme of ['light', 'dark'] as const) {
      await applyTheme(page, theme);
      await expectTextContrastAA(page, '.btn-primary', `login primary action (${theme})`);

      // The hover fill is a second painted background. Both endpoints of the
      // transition must pass, so a reading taken mid-transition is bounded by
      // them and cannot go under the bar.
      await page.locator('form .btn-primary').first().hover();
      await expectTextContrastAA(
        page,
        'form .btn-primary',
        `login primary action, hovered (${theme})`
      );
    }

    // The dashboard editor re-tints its primary action per cycle phase, so the
    // phase-scoped fill is a second painted background the same bar applies to.
    await registerOwnerAndReachDashboard(page, 'visual-contrast');
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    // With the journal on autosave there is no save button, so the dashboard's
    // primary action is the manual cycle start — and it only paints as primary
    // once today is a cycle start. Mark it, so the phase-tinted fill this test
    // exists for is actually on screen.
    await page.locator('[data-dashboard-cycle-start-button]').click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();
    await expect(page.locator('[data-dashboard-editor] .btn-primary')).toBeVisible();

    const editor = page.locator('[data-dashboard-editor]');
    await expect(editor).toHaveAttribute('data-phase', /.+/);

    for (const theme of ['light', 'dark'] as const) {
      await applyTheme(page, theme);
      await expectTextContrastAA(
        page,
        '[data-dashboard-editor] .btn-primary',
        `dashboard primary action, rendered phase (${theme})`
      );

      // `data-phase` drives the per-phase tint and `data-fertility` the
      // fertile-window tint (which outranks phase in the cascade), so setting
      // them directly reaches every fill without seeding a cycle per state.
      // Both attributes are server-rendered, so the next applyTheme reload
      // restores them.
      for (const phase of ['menstrual', 'follicular', 'luteal'] as const) {
        await editor.evaluate((node, value) => {
          node.setAttribute('data-phase', value);
          node.setAttribute('data-fertility', 'not_fertile');
        }, phase);
        await expect(editor).toHaveAttribute('data-phase', phase);
        await expectTextContrastAA(
          page,
          '[data-dashboard-editor] .btn-primary',
          `dashboard primary action, phase ${phase} (${theme})`
        );
      }

      await editor.evaluate((node) => {
        node.setAttribute('data-fertility', 'fertile');
      });
      await expect(editor).toHaveAttribute('data-fertility', 'fertile');
      await expectTextContrastAA(
        page,
        '[data-dashboard-editor] .btn-primary',
        `dashboard primary action, fertile window (${theme})`
      );
    }

    // The calendar day panel's edit action is the screen's primary, so it is a
    // third surface painting `--action-primary` — here over a journal card
    // rather than over the page canvas. A day needs an entry for the panel to
    // render its read view, where that action lives.
    const dayISO = shiftISODate(await todayISOFromDashboard(page), -2);
    const dayMonth = dayISO.slice(0, 7);
    const dayForm = await openCalendarDayEditor(page, dayISO);
    await dayForm.locator('input[name="is_period"]').check();
    await saveDayEditorForm(page, dayISO, dayForm);

    const dayPanelPrimary = '#day-editor [data-action-weight="primary"]';
    for (const theme of ['light', 'dark'] as const) {
      await page.goto(`/calendar?month=${dayMonth}&day=${dayISO}`);
      await applyTheme(page, theme);
      await expect(page.locator(dayPanelPrimary)).toBeVisible();
      await expectTextContrastAA(
        page,
        dayPanelPrimary,
        `calendar day panel primary action (${theme})`
      );
    }
  });

  test('owner pages expose a single h1 and never skip heading levels', async ({ page }) => {
    await registerOwnerAndReachDashboard(page, 'visual-heading-order');

    const assertHeadingStructure = async (label: string): Promise<void> => {
      const result = await page.evaluate(() => {
        const nodes = Array.from(
          document.querySelectorAll('main h1, main h2, main h3, main h4, main h5, main h6'),
        );
        const levels = nodes.map((node) => Number(node.tagName[1]));
        const h1Count = levels.filter((level) => level === 1).length;
        let skipsLevel = false;
        let previous = 0;
        for (const level of levels) {
          if (previous !== 0 && level > previous + 1) skipsLevel = true;
          previous = level;
        }
        return { h1Count, skipsLevel, levels };
      });
      expect(result.h1Count, `${label} must have exactly one <h1> (levels: ${result.levels.join(',')})`).toBe(1);
      expect(
        result.skipsLevel,
        `${label} must not skip a heading level (levels: ${result.levels.join(',')})`,
      ).toBe(false);
    };

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await assertHeadingStructure('/dashboard');

    await page.goto('/stats');
    await expect(page).toHaveURL(/\/stats$/);
    await assertHeadingStructure('/stats');

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await assertHeadingStructure('/settings');

    // Calendar: open a day so the day panel heading (h2 under the page h1) renders.
    const today = await todayISOFromDashboard(page);
    await page.goto('/calendar');
    await expect(page).toHaveURL(/\/calendar/);
    await page.locator(`[data-day-editor-open="${today}"]`).first().click();
    await expect(page.locator('#day-editor h2')).toBeVisible();
    await assertHeadingStructure('/calendar');
  });
});
