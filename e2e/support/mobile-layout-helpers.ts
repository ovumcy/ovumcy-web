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
