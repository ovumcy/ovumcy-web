import { expect, test, type Frame, type Locator, type Page } from './support/fixtures';
import { expectDashboardStatusHeader } from './support/dashboard-helpers';
import { cancelConfirmDialog } from './support/confirm-dialog-helpers';
import { saveInterfaceSettingsForm } from './support/settings-interface-helpers';
import {
  WCAG_AA_GRAPHIC_CONTRAST,
  applyTheme,
  describeContrast,
  measureGraphicContrast,
  measureOverlayContrast,
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

/**
 * The distinct `data-phase` values among the ribbon cells `selector` matches,
 * sorted. Used both to state exactly which phases a sweep resolved (so a
 * fixture edit that makes one stop appearing reddens with a clear diff
 * instead of leaving a narrowed gate silently green) and to drive a
 * per-distinct-phase measurement without walking every one of the axis's up
 * to 60 cells.
 */
async function ribbonPhasesPresent(header: Locator, selector: string): Promise<string[]> {
  const phases = await header.locator(selector).evaluateAll((nodes) =>
    Array.from(
      new Set(
        nodes
          .map((node) => node.getAttribute('data-phase'))
          .filter((phase): phase is string => phase !== null)
      )
    )
  );
  return phases.sort();
}

/**
 * Asserts overlay contrast for `flagAttr` (predicted-flow or start-window)
 * against every DISTINCT phase it is currently painted over, not just the
 * first matching cell — the flag's own colour is fixed, but what it composites
 * against is the phase fill beneath it, which varies by cell, and the start
 * window in particular can straddle any phase (it follows the projected next
 * period, not a fixed offset from today). One cell per distinct phase is
 * enough: two cells of the same phase would only repeat the same composite.
 * Returns the measured ratio per phase so the caller can report exactly what
 * was covered.
 */
async function assertOverlayContrastAcrossPhases(
  header: Locator,
  flagAttr: 'data-predicted-flow' | 'data-start-window',
  themeLabel: 'dark' | 'light'
): Promise<Record<string, number>> {
  const selector = `[data-cycle-ribbon-day][${flagAttr}="true"]`;
  const phasesPresent = await ribbonPhasesPresent(header, selector);
  expect(
    phasesPresent.length,
    `${flagAttr}: no cell carries this flag in the ${themeLabel} theme, so nothing was measured`
  ).toBeGreaterThan(0);

  const ratios: Record<string, number> = {};
  for (const phase of phasesPresent) {
    const cell = header.locator(`${selector}[data-phase="${phase}"]`).first();
    const contrast = await measureOverlayContrast(
      cell,
      `cycle ribbon ${flagAttr} overlay, phase=${phase} (${themeLabel})`
    );
    expect(contrast.worstRatio, describeContrast(contrast)).toBeGreaterThanOrEqual(
      WCAG_AA_GRAPHIC_CONTRAST
    );
    ratios[phase] = contrast.worstRatio;
  }
  return ratios;
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
    // The most recent anchor is 2 days back — inside the default 5-day period —
    // so today sits before the period's own end and the ribbon still has
    // predicted-flow days (day > today, day <= period length) left to draw.
    for (const offset of [-90, -32, -2]) {
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

    // The phase axis this fixture renders, stated explicitly rather than left
    // implicit in the four toHaveCount(1) checks below: the seeded offsets
    // (-90, -32, -2) decide it, and moving any of them — the -2 was moved from
    // -6 to give the overlay assertions a predicted-flow day to measure — can
    // shrink or shift a phase card's day range. 'beyond' is here too: this
    // account's two completed cycles (58 and 30 days) are irregular enough
    // that the predicted start window reaches past the reference cycle length,
    // which is what gives the start-window overlay a transparent-fill cell to
    // paint over. A future fixture edit that makes any of these five stop
    // appearing reddens HERE, by name, rather than only narrowing what the
    // loops below still had left to check.
    const phasesOnAxis = await ribbonPhasesPresent(header, '[data-cycle-ribbon-day]');
    expect(
      phasesOnAxis,
      'phases the ribbon axis actually renders — this fixture must keep producing all five'
    ).toEqual(['beyond', 'follicular', 'luteal', 'menstrual', 'ovulation']);

    // All four phases, measured with 'background' (not 'background-color') so
    // a cell that also carries data-predicted-flow or data-start-window — both
    // painted as background-image, layered on top of the phase's own fill —
    // is not measured as if that overlay were absent. Restricted to a cell
    // carrying neither flag, so this pins the phase's OWN fill, the same
    // quantity input.css's own contrast comment documents.
    const phases = ['menstrual', 'follicular', 'ovulation', 'luteal'] as const;
    const cleanPhaseCell = (phase: (typeof phases)[number]) =>
      header
        .locator(
          `[data-cycle-ribbon-day][data-phase="${phase}"]:not([data-predicted-flow="true"]):not([data-start-window="true"])`
        )
        .first();

    // The two phases a reader looks for — the period itself and the ovulation
    // day — carry the 3:1 non-text floor against the card in both themes. The
    // recessive pair (follicular, luteal) deliberately does not: the phase
    // fill is a redundant graphic — the current phase it paints is already
    // stated as visible text in the status line beside it — and levelling all
    // four collapses them into one luminance where neighbouring phases stop
    // being separable at all (docs: input.css, the calendar phase/state
    // colours comment). The exception is on the two recessive phases
    // specifically, not on skipping their measurement — both are asserted
    // below, just against different floors.
    const exemptFromTheFloor = new Set(['follicular', 'luteal']);

    for (const phase of phases) {
      const cell = cleanPhaseCell(phase);
      await expect(cell).toHaveCount(1);

      const darkContrast = await measureGraphicContrast(
        cell,
        header,
        `cycle ribbon ${phase} (dark)`,
        'background'
      );
      // Dark theme's lightness ladder clears the floor on all four phases.
      expect(darkContrast.worstRatio, describeContrast(darkContrast)).toBeGreaterThanOrEqual(
        WCAG_AA_GRAPHIC_CONTRAST
      );
    }

    // The two flags excluded above are not exempt from the floor — they are the
    // ribbon's only graphical carrier for "predicted, not recorded" and "may
    // start here", per the template's own comment (dashboard.html: "two per-day
    // facts ... exist only here"). Each is measured against what it is actually
    // drawn ON: its own cell's phase fill (or the ribbon track, for a 'beyond'
    // cell with no fill of its own), never the card two layers further down.
    // Measured against every DISTINCT phase the flag currently lands on, not
    // just the first matching cell: the flag's own colour is fixed, but the
    // start window in particular follows the projected next period rather
    // than a fixed day offset, so it can land on any phase, and each one is a
    // different composite the flag has to clear the floor against.
    const darkPredictedFlowRatios = await assertOverlayContrastAcrossPhases(
      header,
      'data-predicted-flow',
      'dark'
    );
    const darkStartWindowRatios = await assertOverlayContrastAcrossPhases(
      header,
      'data-start-window',
      'dark'
    );

    // Anti-vacuity: the same reader has to resolve the light theme's cells too.
    // A reader that silently stopped resolving anything would pass the dark
    // assertions above by measuring nothing at all.
    await applyTheme(page, 'light');

    for (const phase of phases) {
      const cell = cleanPhaseCell(phase);
      const lightContrast = await measureGraphicContrast(
        cell,
        header,
        `cycle ribbon ${phase} (light)`,
        'background'
      );
      expect(lightContrast.stops.length, describeContrast(lightContrast)).toBeGreaterThan(0);
      if (exemptFromTheFloor.has(phase)) {
        continue;
      }
      expect(lightContrast.worstRatio, describeContrast(lightContrast)).toBeGreaterThanOrEqual(
        WCAG_AA_GRAPHIC_CONTRAST
      );
    }

    const lightPredictedFlowRatios = await assertOverlayContrastAcrossPhases(
      header,
      'data-predicted-flow',
      'light'
    );
    const lightStartWindowRatios = await assertOverlayContrastAcrossPhases(
      header,
      'data-start-window',
      'light'
    );

    // Every phase×theme overlay combination this run actually measured, and
    // its ratio — printed rather than discarded so a reader of the test
    // output can see exactly what the assertions above covered, not just that
    // "some cell" passed.
    console.log(
      'cycle ribbon overlay contrast coverage:',
      JSON.stringify({
        predictedFlow: { dark: darkPredictedFlowRatios, light: lightPredictedFlowRatios },
        startWindow: { dark: darkStartWindowRatios, light: lightStartWindowRatios },
      })
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
