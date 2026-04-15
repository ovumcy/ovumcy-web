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
      await page.waitForTimeout(150);

      [elementBox, tabbarBox] = await Promise.all([element.boundingBox(), tabbar.boundingBox()]);
      expect(elementBox, 'expected target element to have a visible bounding box after scrolling').not.toBeNull();
      expect(tabbarBox, 'expected mobile tabbar to have a visible bounding box after scrolling').not.toBeNull();
      elementBottom = elementBox!.y + elementBox!.height;
      tabbarTop = tabbarBox!.y;
    }
  }

  expect(elementBottom).toBeLessThanOrEqual(tabbarTop - minGap);
}
