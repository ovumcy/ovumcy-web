import { expect, test, type Page } from './support/fixtures';
import { applyTheme, WCAG_AA_TEXT_CONTRAST } from './support/contrast-helpers';
import { isoToday, markCycleStartViaAPI, registerAndOnboardWithStartDaysAgo, shiftISODate } from './support/stats-helpers';

/**
 * WEB-190 round 4: a forced-colors primary button keeps its frame, the success
 * toast is an opaque surface, a pre-fertile day has its own legend row and a
 * shape no period state shares, and a failed dashboard save is brought on
 * screen on a desktop viewport.
 */
async function expectForcedColors(page: Page): Promise<void> {
  await page.emulateMedia({ forcedColors: 'active' });
  expect(await page.evaluate(() => matchMedia('(forced-colors: active)').matches)).toBe(true);
}

test('WEB-440: a primary button keeps a visible border under forced colors', async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto('/login');
  await expectForcedColors(page);

  const primary = page.locator('form button[type="submit"].btn-primary').first();
  await expect(primary).toBeVisible();
  const border = await primary.evaluate((el) => {
    const s = getComputedStyle(el);
    return { style: s.borderTopStyle, width: s.borderTopWidth };
  });
  expect(border.style, 'the primary action is framed').toBe('solid');
  expect(border.width).toBe('1px');
});

for (const theme of ['light', 'dark'] as const) {
  test(`WEB-444: the success toast is opaque and readable in the ${theme} theme`, async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto('/login');
    await applyTheme(page, theme);

    const painted = await page.evaluate(() => {
      const node = document.createElement('div');
      node.className = 'status-ok';
      node.textContent = 'Saved';
      document.body.appendChild(node);
      const s = getComputedStyle(node);
      return { background: s.backgroundColor, color: s.color };
    });

    const channels = (value: string): number[] => (/rgba?\(([^)]+)\)/.exec(value)?.[1] ?? '').split(/[ ,/]+/).filter(Boolean).map(Number);
    const bg = channels(painted.background);
    const fg = channels(painted.color);
    expect(bg.length === 3 || bg[3] === 1, `the toast background is opaque, got ${painted.background}`).toBe(true);

    const luminance = ([r, g, b]: number[]): number => {
      const lin = [r, g, b].map((c) => {
        const x = c / 255;
        return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
      });
      return 0.2126 * lin[0] + 0.7152 * lin[1] + 0.0722 * lin[2];
    };
    const [hi, lo] = [luminance(bg), luminance(fg)].sort((a, b) => b - a);
    expect((hi + 0.05) / (lo + 0.05)).toBeGreaterThanOrEqual(WCAG_AA_TEXT_CONTRAST);
  });
}

test.describe('WEB-436: pre-fertile days', () => {
  test('have a legend row and a shape distinct from both period states', async ({ page }) => {
    await registerAndOnboardWithStartDaysAgo(page, 'prefertile-legend', 10);
    // The fertility tiers are withheld until three cycles have been observed,
    // so three completed cycles, 28 days apart, precede the running one.
    await page.goto('/dashboard');
    for (const daysBack of [38, 66, 94]) {
      await markCycleStartViaAPI(page, shiftISODate(isoToday(), -daysBack));
    }
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto('/calendar');

    const cell = page.locator('.calendar-cell-pre-fertile').first();
    await expect(cell, 'the account has pre-fertile days in view').toBeVisible();

    const legend = page.locator('[data-calendar-legend] [data-legend-key="calendar.legend.pre_fertile"]');
    await expect(legend).toBeVisible();
    await expect(legend).toContainText(/before the fertile window \(estimate\)/i);
    await expect(cell).toHaveAttribute('aria-label', /before the fertile window \(estimate\)/i);

    const shape = (selector: string) =>
      page.locator(selector).first().evaluate((el) => {
        const s = getComputedStyle(el);
        return { style: s.borderTopStyle, shadow: s.boxShadow };
      });
    const swatch = await shape('.legend-swatch-pre-fertile');
    const predicted = await page.evaluate(() => {
      const probe = document.createElement('span');
      probe.className = 'legend-swatch legend-swatch-predicted';
      document.body.appendChild(probe);
      return getComputedStyle(probe).borderTopStyle;
    });
    expect(swatch.style, 'not the dashed shape of a predicted period day').not.toBe(predicted);
    expect(swatch.style).toBe('solid');
    expect(swatch.shadow, 'the inner ring is drawn').not.toBe('none');

    const cellShape = await shape('.calendar-cell-pre-fertile');
    expect(cellShape.style).toBe(swatch.style);

    await expectForcedColors(page);
    // Probes carry the classes of each state, so the comparison does not depend
    // on which states this month's grid happens to draw.
    const forced = (classes: string) =>
      page.evaluate((value) => {
        const probe = document.createElement('span');
        probe.className = value;
        document.body.appendChild(probe);
        const s = getComputedStyle(probe);
        const shape = `${s.borderTopStyle}/${s.borderTopWidth}/${s.outlineStyle}/${s.outlineWidth}`;
        probe.remove();
        return shape;
      }, classes);
    const preFertile = await forced('calendar-cell calendar-cell-pre-fertile');
    const period = await forced('calendar-cell calendar-cell-period');
    const predictedPeriod = await forced('calendar-cell calendar-cell-predicted');
    expect(preFertile, 'pre-fertile differs from a recorded period day').not.toBe(period);
    expect(preFertile, 'pre-fertile differs from a predicted period day').not.toBe(predictedPeriod);
    expect(await forced('legend-swatch legend-swatch-pre-fertile').then((v) => v.split('/')[0])).toBe(preFertile.split('/')[0]);
  });
});

test('WEB-435: a failed dashboard save is brought on screen on a desktop viewport', async ({ page, browserErrors }) => {
  browserErrors.allow(/^console\.error: Failed to load resource/, 'the test forces the day PATCH to be refused');
  browserErrors.allow(/^console\.error: Response Status Error Code 500 from \/api\/v1\/days\//, 'the test forces the day PATCH to be refused');
  await registerAndOnboardWithStartDaysAgo(page, 'autosave-failure-visible', 10);
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.goto('/dashboard');

  await page.route('**/api/v1/days/*', async (route) => {
    if (route.request().method() !== 'PATCH') {
      await route.fallback();
      return;
    }
    await route.fulfill({ status: 500, contentType: 'text/html; charset=utf-8', body: '' });
  });

  const status = page.locator('#save-status');
  const mood = page.locator('label[data-mood-option]').first();
  await mood.scrollIntoViewIfNeeded();
  await page.evaluate(() => window.scrollTo(0, 0));
  await mood.click();

  const failure = status.locator('[data-day-save-failed]');
  await expect(failure).toBeVisible();
  await expect(failure, 'the failure notice is inside the viewport').toBeInViewport();
});
