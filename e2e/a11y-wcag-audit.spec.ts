import { expect, test, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { applyTheme, expectTextContrastAA } from './support/contrast-helpers';
import { markCycleStart, shiftISODate, todayISOFromDashboard } from './support/stats-helpers';

/**
 * WCAG 2.2 AA 2.5.8: a pointer target must be at least 24 CSS pixels in both
 * directions unless an exception applies, and none does for a standalone slider.
 */
const WCAG_MINIMUM_TARGET_PX = 24;

const THEMES = ['light', 'dark'] as const;

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

test.describe('WCAG AA audit regressions', () => {
  test('the pre-auth active language pill clears WCAG AA text contrast in both themes', async ({
    page,
  }) => {
    // Auth-free: the switcher is on the login page, which is where the finding
    // was measured (1.98:1 in light, 3.35:1 in dark, on an 11.5px label).
    await page.goto('/login');
    await expect(page).toHaveURL(/\/login$/);

    for (const theme of THEMES) {
      await applyTheme(page, theme);
      // Both gradient stops are solid colours, so each is a background the label
      // really sits on and each has to clear the bar on its own.
      await expectTextContrastAA(
        page,
        '.lang-switch .lang-link-active',
        `active language pill (${theme})`
      );
    }
  });

  test('calendar phase cells keep day numbers above WCAG AA in both themes', async ({ page }) => {
    test.slow();

    await registerOwnerAndReachDashboard(page, 'a11y-calendar-contrast');
    await page.setViewportSize({ width: 1280, height: 900 });

    // A recorded cycle start paints period cells and the fertile window that
    // follows it, so both phase fills are on the page. The window itself is
    // withheld until one cycle has been observed, so the previous cycle's start
    // is seeded too, exactly one 28-day cycle earlier: that is the length the
    // account settings already carry, so the fertile days land where they always
    // did and this case keeps measuring contrast rather than the data tier.
    const today = await todayISOFromDashboard(page);
    await markCycleStart(page, shiftISODate(today, -34));
    await markCycleStart(page, shiftISODate(today, -6));

    await page.goto('/calendar');
    await expect(page).toHaveURL(/\/calendar/);
    await expect(page.locator('.calendar-cell-period').first()).toBeVisible();
    await expect(page.locator('.calendar-cell-fertile').first()).toBeVisible();

    for (const theme of THEMES) {
      await applyTheme(page, theme);
      // The cell inherits the very colour `.calendar-day-number` paints, and it
      // is the element that carries the phase fill, so measuring the cell is
      // measuring the day number against its own background — flattened, which
      // is what a reader sees: the phase colours are laid down at 0.16-0.37
      // alpha, never at full strength.
      await expectTextContrastAA(page, '.calendar-cell-period', `period cell (${theme})`);
      await expectTextContrastAA(page, '.calendar-cell-fertile', `fertile cell (${theme})`);
    }
  });

  test('selected choice tiles clear WCAG AA text contrast in both themes', async ({ page }) => {
    test.slow();

    await registerOwnerAndReachDashboard(page, 'a11y-tile-contrast');
    await page.setViewportSize({ width: 1280, height: 900 });

    for (const theme of THEMES) {
      await applyTheme(page, theme);
      // The mood chip shares the selected-tile declaration; checking the radio
      // in place reaches the :checked fill without saving a day. It is set
      // after the theme, because applying a theme reloads the page.
      await page.evaluate(() => {
        const radio = document.querySelector('input[name="mood"][value="3"]');
        if (radio instanceof HTMLInputElement) {
          radio.checked = true;
        }
      });
      await expectTextContrastAA(
        page,
        '.choice-input:checked + .chip-round',
        `selected mood chip (${theme})`
      );
    }

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    for (const theme of THEMES) {
      await applyTheme(page, theme);
      // Goal, pregnancy test, BBT unit, first day of week, language and theme
      // all render this tile, so one selector covers every selected state.
      await expectTextContrastAA(
        page,
        '.choice-input:checked + .chip-stack',
        `selected choice tile (${theme})`
      );
    }
  });

  test('cycle sliders offer a pointer target of at least 24px', async ({ page }) => {
    await registerOwnerAndReachDashboard(page, 'a11y-slider-target');
    await page.setViewportSize({ width: 1280, height: 900 });

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    const sliders = page.locator('#settings-cycle input[type="range"]');
    const count = await sliders.count();
    expect(count, 'expected the cycle section to render its length sliders').toBeGreaterThan(0);

    for (let index = 0; index < count; index += 1) {
      const slider = sliders.nth(index);
      const id = await slider.getAttribute('id');
      const box = await slider.boundingBox();
      expect(box, `slider ${id} must have a visible box`).not.toBeNull();
      expect(
        box!.height,
        `slider ${id} is ${box!.height}px tall; WCAG 2.2 AA 2.5.8 needs ${WCAG_MINIMUM_TARGET_PX}px`
      ).toBeGreaterThanOrEqual(WCAG_MINIMUM_TARGET_PX);
      // The rail itself stays thin on purpose — only the hit area grew — so the
      // thumb must still be drawn inside the target it belongs to.
      expect(box!.width).toBeGreaterThanOrEqual(WCAG_MINIMUM_TARGET_PX);
    }
  });

  test('the closed confirm dialog is out of the accessibility tree', async ({ page }) => {
    await registerOwnerAndReachDashboard(page, 'a11y-confirm-closed');

    // The dialog's buttons are captionless until it opens, which reads like two
    // unnamed controls. They are not exposed: only a real browser can settle
    // that the closed dialog computes to display:none.
    const modal = page.locator('#confirm-modal');
    await expect(modal).toHaveCSS('display', 'none');
    await expect(modal).toHaveAttribute('aria-hidden', 'true');
    await expect(page.locator('#confirm-modal-cancel')).toBeHidden();
    await expect(page.locator('#confirm-modal-accept')).toBeHidden();
  });
});
