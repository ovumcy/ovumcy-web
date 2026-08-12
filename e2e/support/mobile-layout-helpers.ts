import { expect, type Locator, type Page } from '@playwright/test';

export function mobileTabbar(page: Page): Locator {
  return page.locator('nav.mobile-tabbar');
}

export async function assertNoHorizontalOverflow(page: Page): Promise<void> {
  const overflow = await page.evaluate(() => {
    const root = document.documentElement;
    const hasOverflow = root.scrollWidth > root.clientWidth + 1;
    const viewportWidth = root.clientWidth;
    const offenders = Array.from(document.querySelectorAll<HTMLElement>('body *'))
      .map((element) => {
        const rect = element.getBoundingClientRect();
        return {
          tag: element.tagName.toLowerCase(),
          className: element.className,
          id: element.id,
          text: (element.textContent ?? '').trim().slice(0, 80),
          right: rect.right,
          width: rect.width,
        };
      })
      .filter((entry) => entry.width > 0 && entry.right > viewportWidth + 1)
      .sort((left, right) => right.right - left.right)
      .slice(0, 5);

    return { hasOverflow, viewportWidth, offenders };
  });

  expect(overflow.hasOverflow, JSON.stringify(overflow.offenders, null, 2)).toBe(false);
}

export async function expectVisibleFocusIndicator(locator: Locator): Promise<void> {
  const indicator = await locator.evaluate((node) => {
    const style = window.getComputedStyle(node);
    return {
      outlineStyle: style.outlineStyle,
      outlineWidth: style.outlineWidth,
      boxShadow: style.boxShadow,
      borderColor: style.borderColor,
    };
  });

  const outlineVisible =
    indicator.outlineStyle !== 'none' &&
    indicator.outlineWidth !== '0px' &&
    indicator.outlineWidth !== 'medium';
  const shadowVisible = indicator.boxShadow !== 'none';

  expect(outlineVisible || shadowVisible).toBe(true);
}

function parseColorAlpha(color: string): number | null {
  const normalized = color.trim().toLowerCase();
  if (normalized === 'transparent') {
    return 0;
  }

  const channels = /^rgba?\(([^)]+)\)$/.exec(normalized);
  if (!channels) {
    return null;
  }

  const parts = channels[1]
    .split(/[\s,/]+/)
    .map((part) => part.trim())
    .filter((part) => part.length > 0);
  if (parts.length === 3) {
    return 1;
  }
  if (parts.length !== 4) {
    return null;
  }

  const alpha = parts[3].endsWith('%')
    ? Number.parseFloat(parts[3].slice(0, -1)) / 100
    : Number.parseFloat(parts[3]);
  return Number.isFinite(alpha) ? alpha : null;
}

/**
 * The tabbar floats over scrolling content, so it must paint an opaque layer:
 * either a fully opaque background or a real backdrop filter. A translucent
 * background with `backdrop-filter: none` lets page text read through the bar
 * mid-scroll, which no single-element position assertion can observe.
 */
export async function expectOpaqueMobileTabbar(page: Page): Promise<void> {
  const tabbar = mobileTabbar(page);
  await expect(tabbar).toBeVisible();

  const paint = await tabbar.evaluate((node) => {
    const style = window.getComputedStyle(node);
    const backdropFilter =
      style.backdropFilter || style.getPropertyValue('-webkit-backdrop-filter') || 'none';
    return {
      theme: document.documentElement.getAttribute('data-theme'),
      backgroundColor: style.backgroundColor,
      backgroundImage: style.backgroundImage,
      backdropFilter: backdropFilter.trim(),
      opacity: style.opacity,
    };
  });

  const alpha = parseColorAlpha(paint.backgroundColor);
  const describe = `mobile tabbar paint (${JSON.stringify(paint)})`;

  // An unparsable background is UNKNOWN, never a pass.
  expect(alpha, `${describe}: background-color must be a parsable rgb/rgba value`).not.toBeNull();
  expect(Number.parseFloat(paint.opacity), `${describe}: element opacity must be 1`).toBe(1);

  const backdropFiltered = paint.backdropFilter !== '' && paint.backdropFilter !== 'none';
  expect(
    alpha === 1 || backdropFiltered,
    `${describe}: background alpha must be 1, or a backdrop-filter must be set`
  ).toBe(true);
}

/**
 * Scrolls to the very bottom of the document and asserts the given element —
 * the last interactive one on the page — is not covered by the tabbar there.
 */
export async function expectPageBottomClearsMobileTabbar(
  page: Page,
  element: Locator,
  options?: { minGap?: number }
): Promise<void> {
  const minGap = options?.minGap ?? 8;
  const tabbar = mobileTabbar(page);

  await expect(tabbar).toBeVisible();
  await expect(element).toBeVisible();

  await page.evaluate(() => {
    window.scrollTo(0, document.documentElement.scrollHeight);
  });
  await expect
    .poll(async () =>
      page.evaluate(() => {
        const maxScrollY = Math.max(
          0,
          document.documentElement.scrollHeight - window.innerHeight
        );
        return Math.round(maxScrollY - window.scrollY);
      })
    )
    .toBeLessThanOrEqual(1);

  const [elementBox, tabbarBox] = await Promise.all([element.boundingBox(), tabbar.boundingBox()]);
  expect(elementBox, 'expected the last interactive element to have a visible bounding box').not.toBeNull();
  expect(tabbarBox, 'expected mobile tabbar to have a visible bounding box').not.toBeNull();

  const elementBottom = elementBox!.y + elementBox!.height;
  const horizontallyOverlaps =
    elementBox!.x < tabbarBox!.x + tabbarBox!.width && tabbarBox!.x < elementBox!.x + elementBox!.width;

  if (!horizontallyOverlaps) {
    return;
  }

  expect(
    elementBottom,
    `at the document bottom the element (bottom ${elementBottom}) must clear the tabbar (top ${tabbarBox!.y}) by ${minGap}px`
  ).toBeLessThanOrEqual(tabbarBox!.y - minGap);
}

export type CalendarMonthGridGeometry = {
  viewportHeight: number;
  monthHeaderTop: number;
  gridBottom: number;
  tabbarTop: number;
  rows: number;
  cellCount: number;
  cellWidth: number;
  cellHeight: number;
};

/**
 * Reads the calendar month screen as geometry: where the month header starts,
 * where the last row of cells ends, and how big a single day cell is. The page
 * is read at scroll offset 0 on purpose — the claim under test is what a phone
 * shows before the owner scrolls.
 */
export async function measureCalendarMonthGrid(page: Page): Promise<CalendarMonthGridGeometry> {
  await page.evaluate(() => {
    window.scrollTo(0, 0);
  });

  const geometry = await page.evaluate(() => {
    const monthHeader = document.querySelector('[data-calendar-month-nav]')?.closest('.journal-card');
    const cells = Array.from(document.querySelectorAll<HTMLElement>('#calendar-grid-panel button[data-day]'));
    const tabbar = document.querySelector<HTMLElement>('nav.mobile-tabbar');
    if (!monthHeader || cells.length === 0 || !tabbar) {
      return null;
    }

    const boxes = cells.map((cell) => cell.getBoundingClientRect());
    const rowTops = new Set(boxes.map((box) => Math.round(box.top)));

    return {
      viewportHeight: window.innerHeight,
      monthHeaderTop: monthHeader.getBoundingClientRect().top,
      gridBottom: Math.max(...boxes.map((box) => box.bottom)),
      tabbarTop: tabbar.getBoundingClientRect().top,
      rows: rowTops.size,
      cellCount: cells.length,
      cellWidth: boxes[0].width,
      cellHeight: boxes[0].height,
    };
  });

  expect(geometry, 'expected the calendar month grid, its header card and the mobile tabbar to render').not.toBeNull();
  return geometry!;
}

/**
 * Opens the first month within the next year that fills a full six-row grid —
 * the worst case for vertical density, and the only one worth pinning. A month
 * is addressed by `?month=YYYY-MM`, so the choice is deterministic rather than
 * whatever the current date happens to render.
 */
export async function openLongestCalendarMonth(page: Page): Promise<string> {
  const startedAt = new Date();

  for (let offset = 0; offset < 13; offset += 1) {
    const probe = new Date(startedAt.getFullYear(), startedAt.getMonth() + offset, 1);
    const month = `${probe.getFullYear()}-${String(probe.getMonth() + 1).padStart(2, '0')}`;

    await page.goto(`/calendar?month=${month}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${month}$`));

    const cells = page.locator('#calendar-grid-panel button[data-day]');
    await expect(cells.first()).toBeVisible();
    if ((await cells.count()) === 42) {
      return month;
    }
  }

  throw new Error('no six-row calendar month found within the next year');
}

/**
 * A month grid is only usable on a phone if the whole month — with the month
 * header above it — is on screen without scrolling, and if every day cell is
 * still a real tap target. Both halves belong to one assertion: shrinking the
 * cell until the month fits while the target drops below 44px trades one
 * defect for another.
 */
export async function expectCalendarMonthFitsMobileViewport(
  page: Page,
  options?: { minTapTarget?: number }
): Promise<CalendarMonthGridGeometry> {
  const minTapTarget = options?.minTapTarget ?? 44;
  const geometry = await measureCalendarMonthGrid(page);
  const describe = `calendar month geometry (${JSON.stringify(geometry)})`;

  expect(geometry.rows, `${describe}: expected the six-row worst case`).toBe(6);
  expect(
    geometry.monthHeaderTop,
    `${describe}: the month header must be on screen without scrolling`
  ).toBeGreaterThanOrEqual(0);
  expect(
    geometry.gridBottom,
    `${describe}: the month's last row must sit above the bottom tabbar without scrolling`
  ).toBeLessThanOrEqual(Math.min(geometry.tabbarTop, geometry.viewportHeight));
  expect(
    Math.min(geometry.cellWidth, geometry.cellHeight),
    `${describe}: every day cell stays a ${minTapTarget}px tap target`
  ).toBeGreaterThanOrEqual(minTapTarget);

  return geometry;
}

export async function expectElementAboveMobileTabbar(
  page: Page,
  element: Locator,
  options?: { minGap?: number }
): Promise<void> {
  const minGap = options?.minGap ?? 8;
  const tabbar = mobileTabbar(page);

  await expect(tabbar).toBeVisible();
  await expect(element).toBeVisible();

  let [elementBox, tabbarBox] = await Promise.all([element.boundingBox(), tabbar.boundingBox()]);

  expect(elementBox, 'expected target element to have a visible bounding box').not.toBeNull();
  expect(tabbarBox, 'expected mobile tabbar to have a visible bounding box').not.toBeNull();

  let elementBottom = elementBox!.y + elementBox!.height;
  let tabbarTop = tabbarBox!.y;

  if (elementBottom > tabbarTop - minGap) {
    const scrollState = await page.evaluate(() => {
      return {
        scrollY: window.scrollY,
        maxScrollY: Math.max(0, document.documentElement.scrollHeight - window.innerHeight),
      };
    });
    const remainingScroll = Math.max(0, scrollState.maxScrollY - scrollState.scrollY);
    const neededScroll = Math.max(0, Math.ceil(elementBottom - (tabbarTop - minGap) + 16));

    if (remainingScroll > 0 && neededScroll > 0) {
      await page.evaluate((delta) => {
        window.scrollBy(0, delta);
      }, Math.min(remainingScroll, neededScroll));
      await expect.poll(() => element.boundingBox()).not.toBeNull();

      [elementBox, tabbarBox] = await Promise.all([element.boundingBox(), tabbar.boundingBox()]);
      expect(elementBox, 'expected target element to have a visible bounding box after scrolling').not.toBeNull();
      expect(tabbarBox, 'expected mobile tabbar to have a visible bounding box after scrolling').not.toBeNull();
      elementBottom = elementBox!.y + elementBox!.height;
      tabbarTop = tabbarBox!.y;
    }
  }

  expect(elementBottom).toBeLessThanOrEqual(tabbarTop - minGap);
}
