import { expect, test, type Page } from './support/fixtures';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { applyTheme, WCAG_AA_GRAPHIC_CONTRAST, WCAG_AA_TEXT_CONTRAST } from './support/contrast-helpers';

async function registerAndOpenCalendar(page: Page): Promise<void> {
  await registerOwnerViaUI(page, createCredentials('contrast-tokens'));
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);
  await page.goto('/calendar');
  await expect(page.locator('.calendar-cell-today')).toBeVisible();
}

// Computed colours the page paints, read in the page so every var() is resolved.
// Alpha is flattened over the card colour, which is what sits under all of them.
async function measure(page: Page) {
  return page.evaluate(() => {
    type Rgb = [number, number, number];
    const parse = (value: string): { rgb: Rgb; a: number } => {
      const m = /rgba?\(([^)]+)\)/.exec(value);
      if (!m) throw new Error(`unparsable colour: ${value}`);
      const parts = m[1].split(/[ ,/]+/).filter(Boolean).map(Number);
      return { rgb: [parts[0], parts[1], parts[2]], a: parts[3] ?? 1 };
    };
    const probe = document.createElement('span');
    document.body.appendChild(probe);
    const card = parse(
      (() => {
        probe.style.backgroundColor = 'var(--bg-card)';
        return getComputedStyle(probe).backgroundColor;
      })()
    );
    probe.remove();
    const flatten = (value: string): Rgb => {
      const { rgb, a } = parse(value);
      return rgb.map((c, i) => c * a + card.rgb[i] * (1 - a)) as Rgb;
    };
    const lum = ([r, g, b]: Rgb) => {
      const lin = (c: number) => {
        const s = c / 255;
        return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
      };
      return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
    };
    const ratio = (a: Rgb, b: Rgb) => {
      const [hi, lo] = [lum(a), lum(b)].sort((x, y) => y - x);
      return (hi + 0.05) / (lo + 0.05);
    };

    const swatches: Record<string, number> = {};
    for (const el of document.querySelectorAll<HTMLElement>('.legend-swatch')) {
      const name = [...el.classList].find((c) => c.startsWith('legend-swatch-')) ?? 'legend-swatch';
      swatches[name] = ratio(flatten(getComputedStyle(el).borderTopColor), card.rgb);
    }

    const out = document.querySelector<HTMLElement>('.calendar-cell-out');
    const outNumber = out?.querySelector<HTMLElement>('.calendar-day-out');
    const outRatio =
      out && outNumber
        ? ratio(flatten(getComputedStyle(outNumber).color), card.rgb)
        : null;
    const outOpacity = out ? Number(getComputedStyle(out).opacity) : null;

    const amber = document.createElement('p');
    amber.className = 'warning-amber';
    amber.textContent = 'x';
    document.body.appendChild(amber);
    const amberRatio = ratio(flatten(getComputedStyle(amber).color), card.rgb);
    amber.remove();

    return { swatches, outRatio, outOpacity, amberRatio };
  });
}

async function focusRingRatio(page: Page): Promise<{ ratio: number; style: string; todayShadowDiffers: boolean }> {
  const today = page.locator('.calendar-cell-today');
  await today.focus();
  await page.keyboard.press('Shift+Tab');
  await page.keyboard.press('Tab');
  return today.evaluate((el) => {
    const cs = getComputedStyle(el);
    const parts = /rgba?\(([^)]+)\)/.exec(cs.outlineColor)![1].split(/[ ,/]+/).map(Number);
    const lin = (c: number) => {
      const s = c / 255;
      return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
    };
    const lum = (r: number, g: number, b: number) => 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
    const page = getComputedStyle(document.body).backgroundColor.match(/\d+(\.\d+)?/g)!.map(Number);
    const a = lum(parts[0], parts[1], parts[2]);
    const b = lum(page[0], page[1], page[2]);
    return {
      ratio: (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05),
      style: `${cs.outlineStyle} ${cs.outlineWidth}`,
      todayShadowDiffers: cs.outlineStyle === 'solid' && cs.outlineWidth !== '0px',
    };
  });
}

test.describe('WEB-260 contrast tokens', () => {
  test('legend swatch borders, out-of-month numbers, amber hint and focus ring clear WCAG in both themes', async ({
    page,
  }) => {
    await registerAndOpenCalendar(page);

    for (const theme of ['light', 'dark'] as const) {
      await applyTheme(page, theme);
      const result = await measure(page);

      expect(Object.keys(result.swatches).length, `${theme}: legend swatches present`).toBeGreaterThan(0);
      for (const [name, ratio] of Object.entries(result.swatches)) {
        expect(ratio, `${theme}: ${name} border`).toBeGreaterThanOrEqual(WCAG_AA_GRAPHIC_CONTRAST);
      }

      expect(result.outOpacity, `${theme}: out-of-month cell must not fade its number with opacity`).toBe(1);
      expect(result.outRatio, `${theme}: out-of-month day number`).toBeGreaterThanOrEqual(WCAG_AA_TEXT_CONTRAST);
      expect(result.amberRatio, `${theme}: .warning-amber`).toBeGreaterThanOrEqual(WCAG_AA_TEXT_CONTRAST);

      const ring = await focusRingRatio(page);
      expect(ring.todayShadowDiffers, `${theme}: focused today cell draws a solid outline (${ring.style})`).toBe(true);
      expect(ring.ratio, `${theme}: focus ring`).toBeGreaterThanOrEqual(WCAG_AA_GRAPHIC_CONTRAST);
    }
  });
});
