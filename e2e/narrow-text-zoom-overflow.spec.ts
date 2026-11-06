import { expect, test, type Page } from './support/fixtures';
import {
  isoToday,
  markCycleStartViaAPI,
  registerAndOnboardWithStartDaysAgo,
  shiftISODate,
} from './support/stats-helpers';
import { saveInterfaceSettingsForm } from './support/settings-interface-helpers';
import { apiOriginHeader } from './support/auth-helpers';

/**
 * Narrow phones at 200% text (WEB-441, WEB-442, WEB-439, WEB-496): the document
 * never scrolls sideways, and the danger-zone re-authentication row stacks its
 * field over its button instead of squeezing the two side by side.
 */
type Lang = 'de' | 'en' | 'es' | 'fr' | 'it' | 'ru';

/** Near the catalogue's 40-character cap: about the longest name a symptom row carries. */
const LONG_SYMPTOM_NAME = 'Тянущая боль внизу живота и в пояснице';

/**
 * Three completed cycles. With `symptoms`, a built-in and a long custom symptom
 * are logged twice in the last cycle, so /stats renders the cycle chart's
 * "recent cycles" caption and every symptom panel's frequency (WEB-496).
 */
async function seedAccountWithCycles(
  page: Page,
  prefix: string,
  { lang = 'ru', symptoms = false }: { lang?: Lang; symptoms?: boolean } = {},
): Promise<void> {
  await registerAndOnboardWithStartDaysAgo(page, prefix, 60);
  const today = isoToday();
  // 18-day gaps stay clear of the short-gap confirmation (see stats-insights.spec.ts).
  for (const daysAgo of [42, 24, 6]) {
    await markCycleStartViaAPI(page, shiftISODate(today, -daysAgo));
  }
  if (symptoms) {
    await page.goto('/dashboard');
    const csrf = (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
    const headers = { ...apiOriginHeader(page), 'X-CSRF-Token': csrf };
    const created = await page.request.post('/api/v1/symptoms', { headers, form: { name: LONG_SYMPTOM_NAME } });
    expect(created.status(), 'create the long custom symptom').toBeLessThan(400);

    await page.goto('/dashboard');
    const options = page.locator('fieldset[data-dashboard-section="symptoms"] input[name="symptom_ids"]');
    const builtInID = await options.first().getAttribute('value');
    const customID = await page
      .locator(`fieldset[data-dashboard-section="symptoms"] label.choice-option:has-text("${LONG_SYMPTOM_NAME}") input[name="symptom_ids"]`)
      .getAttribute('value');
    expect(builtInID, 'the dashboard lists a built-in symptom').toMatch(/^\d+$/);
    expect(customID, 'the dashboard lists the custom symptom').toMatch(/^\d+$/);
    for (const daysAgo of [15, 12]) {
      const response = await page.request.put(`/api/v1/days/${shiftISODate(today, -daysAgo)}`, {
        headers: { ...headers, 'Content-Type': 'application/json' },
        data: { symptom_ids: [Number(builtInID), Number(customID)] },
      });
      expect(response.status(), `save symptoms ${daysAgo} days ago`).toBeLessThan(400);
    }
  }

  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/settings');
  // Re-picking the current language is not a change, and the form never submits.
  if ((await page.locator('html').getAttribute('lang')) === lang) {
    return;
  }
  const section = page.locator('#settings-interface');
  if ((await section.getAttribute('open')) === null) {
    await section.locator('summary').click();
  }
  await section.locator(`[data-settings-interface-language-option="${lang}"] .chip-stack`).click();
  await saveInterfaceSettingsForm(page, { expectSuccessFlash: true });
  await expect(page.locator('html')).toHaveAttribute('lang', lang);
}

/**
 * Symptom rows, in every /stats panel, whose frequency has dropped below the
 * name's box: at normal text it stays on the name's line, however long the name.
 */
async function frequenciesBelowTheirName(page: Page): Promise<{ rows: number; dropped: string[] }> {
  return page.evaluate(() => {
    const rows = Array.from(document.querySelectorAll<HTMLElement>('.stats-symptom-row, .phase-symptom-row')).filter(
      (row) => row.getClientRects().length > 0,
    );
    const dropped = rows
      .filter((row) => {
        const name = row.querySelector('.stats-symptom-name')?.getBoundingClientRect();
        const frequency = row.querySelector('.stats-symptom-frequency')?.getBoundingClientRect();
        return !name || !frequency || frequency.top >= name.bottom;
      })
      .map((row) => row.textContent?.replace(/\s+/g, ' ').trim() ?? '');
    return { rows: rows.length, dropped };
  });
}

/** Every collapsible section open too: the settings toggle rows only overflow expanded. */
async function zoomTextTo200(page: Page): Promise<void> {
  await page.evaluate(() => {
    document.documentElement.style.fontSize = '32px';
    document.querySelectorAll('details').forEach((section) => {
      section.open = true;
    });
  });
}

async function horizontalOverflow(page: Page): Promise<{ scrollWidth: number; clientWidth: number; wide: string[] }> {
  return page.evaluate(() => {
    const root = document.documentElement;
    const wide = Array.from(document.body.querySelectorAll<HTMLElement>('*'))
      .filter((el) => el.getBoundingClientRect().right > root.clientWidth + 1 && el.getClientRects().length > 0)
      .filter((el) => !el.closest('[data-settings-section-nav], .sr-only'))
      .slice(0, 5)
      .map((el) => `${el.tagName}.${el.className}`);
    // Text overhanging a box that itself fits (one unbreakable word) shows up
    // only on the text node, never on an element's rect.
    const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
    const range = document.createRange();
    for (let node = walker.nextNode(); node && wide.length < 8; node = walker.nextNode()) {
      const parent = node.parentElement;
      if (!parent || parent.closest('[data-settings-section-nav], .sr-only')) continue;
      range.selectNodeContents(node);
      const box = range.getBoundingClientRect();
      if (box.width > 0 && box.right > root.clientWidth + 1) {
        wide.push(`${parent.tagName}.${parent.className} "${node.textContent?.trim().slice(0, 40)}"`);
      }
    }
    return { scrollWidth: root.scrollWidth, clientWidth: root.clientWidth, wide };
  });
}

const statsLanguages: Lang[] = ['de', 'en', 'es', 'fr', 'it', 'ru'];
const pages: { path: string; lang: Lang }[] = [
  ...statsLanguages.map((lang) => ({ path: '/stats', lang })),
  { path: '/dashboard', lang: 'ru' },
  { path: '/settings', lang: 'ru' },
];

for (const width of [320, 375]) {
  test.describe(`200% text at ${width}px`, () => {
    for (const { path, lang } of pages) {
      test(`${path} (${lang}) does not scroll sideways`, async ({ page }) => {
        const stats = path === '/stats';
        await seedAccountWithCycles(page, `zoom-${width}-${lang}-${path.slice(1)}`, { lang, symptoms: stats });
        await page.setViewportSize({ width, height: 700 });
        await page.goto(path);
        if (stats) {
          // The WEB-496 offenders must be on the page, or the width check proves nothing.
          await expect(page.locator('[data-stats-recent-cycles-caption]')).toBeVisible();
          await expect(
            page.locator('[data-stats-last-cycle-symptoms] .stats-symptom-name', { hasText: LONG_SYMPTOM_NAME }),
          ).toBeVisible();
          const { rows, dropped } = await frequenciesBelowTheirName(page);
          expect(rows, 'symptom rows rendered').toBeGreaterThan(1);
          expect(dropped, 'at normal text every frequency shares its name line').toEqual([]);
        }
        await zoomTextTo200(page);

        const { scrollWidth, clientWidth, wide } = await horizontalOverflow(page);
        expect(scrollWidth, `document is wider than the viewport; offenders: ${wide.join(', ')}`).toBeLessThanOrEqual(
          clientWidth,
        );
        const tabbar = await page.locator('nav.mobile-tabbar').boundingBox();
        expect(tabbar && tabbar.x + tabbar.width, 'the tab bar stays inside the viewport').toBeLessThanOrEqual(width);
      });
    }
  });
}

test('the danger-zone password rows stack the field over the button on a phone (WEB-439)', async ({ page }) => {
  await seedAccountWithCycles(page, 'zoom-danger-rows');
  await page.setViewportSize({ width: 320, height: 700 });
  await page.goto('/settings');
  const danger = page.locator('#settings-danger-zone');
  if ((await danger.count()) > 0 && (await danger.getAttribute('open')) === null) {
    await danger.locator('summary').click();
  }

  for (const id of ['settings-clear-data-password', 'settings-delete-password']) {
    const input = page.locator(`#${id}`);
    await input.scrollIntoViewIfNeeded();
    const geometry = await input.evaluate((el) => {
      const row = el.closest('.settings-form-actions')!;
      const button = row.querySelector<HTMLElement>('button[type="submit"]')!;
      const field = el.getBoundingClientRect();
      const action = button.getBoundingClientRect();
      return {
        // Painted, not spelled: a class name left in the markup with no rule behind
        // it still lays the row out as a block and would pass the geometry alone.
        fieldBasis: getComputedStyle(el.closest('.settings-reauth-field')!).flexBasis,
        fieldWidth: field.width,
        rowWidth: row.getBoundingClientRect().width,
        fieldBottom: field.bottom,
        buttonTop: action.top,
        buttonHeight: action.height,
      };
    });
    expect(geometry.fieldBasis, `${id} wrapper claims the whole line`).toBe('100%');
    expect(geometry.fieldWidth, `${id} takes the row`).toBeGreaterThan(geometry.rowWidth * 0.7);
    expect(geometry.buttonTop, `${id} button sits below the field`).toBeGreaterThanOrEqual(geometry.fieldBottom);
    expect(geometry.buttonHeight, `${id} button is not stretched into an oval`).toBeLessThan(70);
  }
});
