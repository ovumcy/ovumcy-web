import { expect, test, type Frame, type Page } from '@playwright/test';
import { expectDashboardStatusHeader } from './support/dashboard-helpers';
import { cancelConfirmDialog } from './support/confirm-dialog-helpers';
import { saveInterfaceSettingsForm } from './support/settings-interface-helpers';
import {
  WCAG_AA_GRAPHIC_CONTRAST,
  applyTheme,
  describeContrast,
  measureGraphicContrast,
} from './support/contrast-helpers';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  logoutViaAPI,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import {
  isoToday,
  markCycleStartViaAPI,
  registerAndOnboardWithStartDaysAgo,
  shiftISODate,
} from './support/stats-helpers';

async function registerAndReachDashboard(
  page: Page,
  prefix: string
): Promise<{ email: string; password: string }> {
  const credentials = createCredentials(prefix);

  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);

  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);
  return credentials;
}

/** What the cold-start probe records inside the page under test. */
interface PaintTiming {
  /** `performance.now()` when html[data-theme] was first written. */
  themeSetAt: number | null;
  /** The value it was written with. */
  theme: string | null;
  /** `startTime` of the first paint entry the browser reported. */
  firstPaintAt: number | null;
}

/**
 * Flips the emulated system colour scheme and returns once the page has seen the
 * change. A "the theme must NOT have moved" assertion needs a settled page —
 * asserted straight after `emulateMedia` it would also pass while the change was
 * still on its way.
 */
async function awaitSystemColorSchemeFlip(page: Page, scheme: 'light' | 'dark'): Promise<void> {
  // Emulating the scheme the page already reports fires no change event, and the
  // wait below would hang until the test timeout instead of saying why.
  const alreadyDark = await page.evaluate(
    () => window.matchMedia('(prefers-color-scheme: dark)').matches
  );
  expect(
    alreadyDark,
    `the page already reports prefers-color-scheme: ${scheme}, so flipping to it is not a flip`
  ).toBe(scheme === 'light');

  await page.evaluate(() => {
    (window as unknown as { __ovumcySchemeFlip: Promise<void> }).__ovumcySchemeFlip =
      new Promise<void>((resolve) => {
        window
          .matchMedia('(prefers-color-scheme: dark)')
          .addEventListener('change', () => resolve(), { once: true });
      });
  });
  await page.emulateMedia({ colorScheme: scheme });
  await page.evaluate(
    () => (window as unknown as { __ovumcySchemeFlip: Promise<void> }).__ovumcySchemeFlip
  );
  // One animation frame later, every other listener on the same query has run.
  await page.evaluate(
    () =>
      new Promise<void>((resolve) => {
        requestAnimationFrame(() => resolve());
      })
  );
}

test.describe('Theme mode', () => {
  test('theme toggle switches mode and persists between pages', async ({ page, context }) => {
    await registerAndReachDashboard(page, 'theme-mode');

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    const interfaceForm = page.locator('[data-settings-interface-form]');
    const html = page.locator('html');
    const lightOption = interfaceForm.locator('[data-settings-interface-theme-option="light"]');
    const darkOption = interfaceForm.locator('[data-settings-interface-theme-option="dark"]');
    const saveButton = interfaceForm.locator('[data-settings-interface-save]');
    const discardButton = interfaceForm.locator('[data-settings-interface-discard]');
    await expect(lightOption).toBeVisible();
    await expect(darkOption).toBeVisible();
    await expect(saveButton).toBeDisabled();
    await expect(discardButton).toBeDisabled();

    const initialTheme = await html.getAttribute('data-theme');
    expect(initialTheme === 'light' || initialTheme === 'dark').toBeTruthy();
    const storedBefore = await page.evaluate(() => window.localStorage.getItem('ovumcy_theme'));

    const nextTheme = initialTheme === 'dark' ? 'light' : 'dark';
    const nextOption = nextTheme === 'dark' ? darkOption : lightOption;
    const previousOption = nextTheme === 'dark' ? lightOption : darkOption;
    await nextOption.locator('.chip-stack').click();

    await expect(html).toHaveAttribute('data-theme', nextTheme);
    await expect(nextOption).toHaveAttribute('data-selected', 'true');
    await expect(previousOption).toHaveAttribute('data-selected', 'false');
    await expect(saveButton).toBeEnabled();
    await expect(discardButton).toBeEnabled();
    await expect
      .poll(async () => page.evaluate(() => window.localStorage.getItem('ovumcy_theme')))
      .toBe(storedBefore);

    // Cancelling the unsaved-changes prompt must leave the page where it is. A
    // URL assertion is already true the moment it runs, so it would pass just as
    // well with the guarded navigation still pending: record what the main frame
    // actually navigates to across the whole window instead.
    const brandLink = page.locator('a.brand-mark');
    const leaveTarget = new URL((await brandLink.getAttribute('href')) ?? '', page.url()).pathname;
    expect(leaveTarget).toBe('/dashboard');

    const navigatedTo: string[] = [];
    const recordNavigation = (frame: Frame) => {
      if (frame === page.mainFrame()) {
        navigatedTo.push(frame.url());
      }
    };
    page.on('framenavigated', recordNavigation);
    try {
      await brandLink.click();
      await cancelConfirmDialog(page);
      await expect(html).toHaveAttribute('data-theme', nextTheme);

      // Concrete signal that the guarded link has had its chance: discarding the
      // draft is a real interaction with — and a state transition on — the very
      // document the accept branch would have navigated away from.
      await discardButton.click();
      await expect(html).toHaveAttribute('data-theme', String(initialTheme));
    } finally {
      page.off('framenavigated', recordNavigation);
    }
    expect(navigatedTo, `cancelling the prompt must not navigate to ${leaveTarget}`).toEqual([]);

    await expect(nextOption).toHaveAttribute('data-selected', 'false');
    await expect(previousOption).toHaveAttribute('data-selected', 'true');
    await expect(saveButton).toBeDisabled();
    await expect(discardButton).toBeDisabled();
    await expect
      .poll(async () => page.evaluate(() => window.localStorage.getItem('ovumcy_theme')))
      .toBe(storedBefore);

    await nextOption.locator('.chip-stack').click();
    await expect(saveButton).toBeEnabled();
    // The success flash is the only server-backed readback this scenario has:
    // the theme itself lives in client storage by design, so what the flash
    // proves is that the endpoint accepted the save rather than the page going
    // on showing a preference it refused.
    await saveInterfaceSettingsForm(page, { expectSuccessFlash: true });
    // Kept as the draft's own end state, no longer as the wait for the save.
    await expect(saveButton).toBeDisabled();
    await expect(html).toHaveAttribute('data-theme', nextTheme);
    await expect
      .poll(async () => page.evaluate(() => window.localStorage.getItem('ovumcy_theme')))
      .toBe(nextTheme);

    // The save is answered, so this navigation has nothing left to abort. What
    // the two pages below check is the client half — where the theme lives.
    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(html).toHaveAttribute('data-theme', nextTheme);
    await expectDashboardStatusHeader(page);

    const secondPage = await context.newPage();
    await secondPage.goto('/privacy');
    await expect(secondPage.locator('html')).toHaveAttribute('data-theme', nextTheme);
    await secondPage.close();

    await logoutViaAPI(page);
  });

  test('dark theme keeps the cycle ribbon readable in the status header', async ({ page }) => {
    // The ribbon draws a phase map, so it needs an account whose data supports
    // one: onboarding's MinDate is 60 days back, and the cycle-start API has no
    // past bound, so the older anchors are backfilled through it.
    await registerAndOnboardWithStartDaysAgo(page, 'theme-dark-ribbon', 60);
    const today = isoToday();
    for (const offset of [-90, -32, -6]) {
      await markCycleStartViaAPI(page, shiftISODate(today, offset));
    }

    await applyTheme(page, 'dark');
    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    const header = await expectDashboardStatusHeader(page);
    await expect(header.locator('[data-dashboard-cycle-ribbon]')).toHaveAttribute(
      'data-cycle-ribbon-visible',
      'true'
    );
    // The two phases a reader looks for — the period itself and the ovulation
    // day — carry the 3:1 non-text floor against the card in both themes. The
    // recessive pair (follicular, luteal) deliberately does not: the band is a
    // redundant aria-hidden graphic whose every fact is stated in the status
    // line, and levelling all four collapses them into one luminance where
    // neighbouring phases stop being separable at all.
    const menstrual = header.locator('[data-cycle-ribbon-day][data-phase="menstrual"]').first();
    await expect(menstrual).toHaveCount(1);

    const darkContrast = await measureGraphicContrast(
      menstrual,
      header,
      'cycle ribbon menstrual (dark)',
      'background-color'
    );
    expect(darkContrast.worstRatio, describeContrast(darkContrast)).toBeGreaterThanOrEqual(
      WCAG_AA_GRAPHIC_CONTRAST
    );

    // Anti-vacuity: the same reader has to resolve the light theme's cell too.
    // A reader that silently stopped resolving anything would pass the dark
    // assertion above by measuring nothing at all.
    await applyTheme(page, 'light');
    const lightContrast = await measureGraphicContrast(
      menstrual,
      header,
      'cycle ribbon menstrual (light)',
      'background-color'
    );
    expect(lightContrast.stops.length, describeContrast(lightContrast)).toBeGreaterThan(0);
    expect(lightContrast.worstRatio, describeContrast(lightContrast)).toBeGreaterThanOrEqual(
      WCAG_AA_GRAPHIC_CONTRAST
    );

    await logoutViaAPI(page);
  });

  test('the system theme option follows prefers-color-scheme live', async ({ page }) => {
    await registerAndReachDashboard(page, 'theme-system');
    await page.emulateMedia({ colorScheme: 'light' });

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    const interfaceForm = page.locator('[data-settings-interface-form]');
    const html = page.locator('html');
    const systemOption = interfaceForm.locator('[data-settings-interface-theme-option="system"]');
    const lightOption = interfaceForm.locator('[data-settings-interface-theme-option="light"]');
    const saveButton = interfaceForm.locator('[data-settings-interface-save]');
    const successFlash = page.locator(
      '[data-flash-key="settings.success.interface_updated"][data-flash-status="success"]'
    );
    await expect(systemOption).toBeVisible();

    await systemOption.locator('.chip-stack').click();
    await expect(systemOption).toHaveAttribute('data-selected', 'true');
    // "system" resolves at apply time: `data-theme` keeps carrying light or dark,
    // so every stylesheet rule written for the two of them still matches.
    await expect(html).toHaveAttribute('data-theme', 'light');

    await saveButton.click();
    // The success flash is rendered by the server, so it also proves the
    // interface endpoint accepts the third value instead of the form having only
    // written local storage on its way to a 400.
    await expect(successFlash).toBeVisible();
    await expect
      .poll(async () => page.evaluate(() => window.localStorage.getItem('ovumcy_theme')))
      .toBe('system');
    await expect(systemOption).toHaveAttribute('data-selected', 'true');

    // Live, with no navigation: the OS flipping to dark at sunset reaches a page
    // that is already open.
    await page.emulateMedia({ colorScheme: 'dark' });
    await expect(html).toHaveAttribute('data-theme', 'dark');
    await page.emulateMedia({ colorScheme: 'light' });
    await expect(html).toHaveAttribute('data-theme', 'light');

    // ... and it is resolved again on the next page, not carried over.
    await page.emulateMedia({ colorScheme: 'dark' });
    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(html).toHaveAttribute('data-theme', 'dark');

    // An explicit preference outranks the system and stops following it. Saved
    // while the system reports light, so the flip below is a real state change.
    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await page.emulateMedia({ colorScheme: 'light' });
    await expect(html).toHaveAttribute('data-theme', 'light');
    await lightOption.locator('.chip-stack').click();
    await saveButton.click();
    await expect(successFlash).toBeVisible();
    await expect(html).toHaveAttribute('data-theme', 'light');
    await expect
      .poll(async () => page.evaluate(() => window.localStorage.getItem('ovumcy_theme')))
      .toBe('light');

    await awaitSystemColorSchemeFlip(page, 'dark');
    await expect(html).toHaveAttribute('data-theme', 'light');

    await logoutViaAPI(page);
  });

  test('a cold start with a saved dark theme is dark on the first paint', async ({
    page,
    context,
  }) => {
    // Seeding through a page on the origin, then opening a second one, is the
    // cold start an owner gets: storage already holds the preference and the new
    // document has to honour it before its first frame.
    await page.goto('/login');
    await expect(page).toHaveURL(/\/login$/);
    await page.evaluate(() => window.localStorage.setItem('ovumcy_theme', 'dark'));

    const cold = await context.newPage();
    await cold.addInitScript(() => {
      const timing: PaintTiming = { themeSetAt: null, theme: null, firstPaintAt: null };
      (window as unknown as { __ovumcyPaintTiming: PaintTiming }).__ovumcyPaintTiming = timing;

      new MutationObserver((records) => {
        for (const record of records) {
          if (record.attributeName !== 'data-theme' || timing.themeSetAt !== null) {
            continue;
          }
          timing.themeSetAt = performance.now();
          timing.theme = document.documentElement.getAttribute('data-theme');
        }
        // The document node is observed rather than documentElement: an init
        // script runs before the parser has created <html>.
      }).observe(document, { attributes: true, subtree: true, attributeFilter: ['data-theme'] });

      new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          if (timing.firstPaintAt === null) {
            timing.firstPaintAt = entry.startTime;
          }
        }
      }).observe({ type: 'paint', buffered: true });
    });

    try {
      await cold.goto('/login');
      await expect(cold.locator('html')).toHaveAttribute('data-theme', 'dark');
      await cold.waitForLoadState('load');

      const readTiming = async (): Promise<PaintTiming> =>
        cold.evaluate(
          () => (window as unknown as { __ovumcyPaintTiming: PaintTiming }).__ovumcyPaintTiming
        );

      // A missing paint entry is unknown, never a pass: without it there is no
      // moment to compare the theme against.
      await expect
        .poll(async () => (await readTiming()).firstPaintAt !== null, {
          message: 'the browser reported no paint entry, so the first-paint moment is unknown',
        })
        .toBe(true);

      const timing = await readTiming();
      expect(
        timing.themeSetAt,
        'nothing ever set html[data-theme], so the theme cannot have been applied before paint'
      ).not.toBeNull();
      expect(timing.theme, 'the first value written to html[data-theme]').toBe('dark');
      expect(
        Number(timing.themeSetAt),
        `html[data-theme]=dark was applied at ${String(timing.themeSetAt)}ms but the first paint ` +
          `happened at ${String(timing.firstPaintAt)}ms — the gap between them is the white flash`
      ).toBeLessThanOrEqual(Number(timing.firstPaintAt));

      // And the frame that paint produced was the dark canvas, not a light one
      // repainted afterwards.
      const canvas = await cold.evaluate(
        () => window.getComputedStyle(document.body).backgroundColor
      );
      expect(canvas, 'the painted page background in the dark theme').toBe('rgb(24, 20, 31)');
    } finally {
      await cold.close();
    }
  });
});
