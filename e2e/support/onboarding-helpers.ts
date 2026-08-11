import { expect, type Locator, type Page } from '@playwright/test';

/**
 * Step 1 of onboarding holds exactly one mechanism for the last-period-start
 * value: the month picker. Every spec that has to reach a specific date goes
 * through these helpers rather than typing into a field — there is no field to
 * type into, and the picker's month paging is the only way to reach a date
 * outside the month it opens on.
 */
export function onboardingPicker(page: Page): Locator {
  return page.locator('[data-onboarding-picker]');
}

export function onboardingDayCell(page: Page, isoDate: string): Locator {
  return page.locator(`[data-onboarding-day-option][data-onboarding-day-value="${isoDate}"]`);
}

export function onboardingShortcut(page: Page, name: 'today' | 'yesterday'): Locator {
  return onboardingPicker(page).locator(`[data-onboarding-shortcut="${name}"]`);
}

/**
 * Pages the picker to `isoDate`'s month and activates its day cell from the
 * keyboard, then proves the transport input carries the ISO value the server
 * expects. The month paging is driven by the picker's own
 * `data-onboarding-visible-month` state, so the loop cannot spin on a click
 * that did nothing.
 */
export async function selectOnboardingStartDate(page: Page, isoDate: string): Promise<void> {
  const picker = onboardingPicker(page);
  await expect(picker).toBeVisible();

  const targetMonth = isoDate.slice(0, 7);
  for (let attempt = 0; attempt < 24; attempt += 1) {
    const visibleMonth = String(await picker.getAttribute('data-onboarding-visible-month'));
    if (visibleMonth === targetMonth) {
      break;
    }

    const direction = visibleMonth < targetMonth ? 'next' : 'prev';
    const monthButton = picker.locator(`[data-onboarding-month-${direction}]`);
    await expect(monthButton).toBeEnabled();
    await monthButton.click();
    await expect(picker).not.toHaveAttribute('data-onboarding-visible-month', visibleMonth);
  }
  await expect(picker).toHaveAttribute('data-onboarding-visible-month', targetMonth);

  const cell = onboardingDayCell(page, isoDate);
  await expect(cell).toBeEnabled();
  await cell.focus();
  await page.keyboard.press('Enter');

  await expect(cell).toHaveAttribute('aria-pressed', 'true');
  await expect(page.locator('#last-period-start')).toHaveValue(isoDate);
}
