import { type Locator } from '@playwright/test';

function splitISODate(isoDate: string): { year: string; month: string; day: string } {
  const parts = String(isoDate || '').trim().split('-');
  if (parts.length !== 3) {
    throw new Error(`Invalid ISO date: ${isoDate}`);
  }

  const [year, month, day] = parts;
  if (!/^\d{4}$/.test(year) || !/^\d{2}$/.test(month) || !/^\d{2}$/.test(day)) {
    throw new Error(`Invalid ISO date: ${isoDate}`);
  }

  return { year, month, day };
}

export function dateFieldRoot(field: Locator): Locator {
  return field.locator('xpath=ancestor-or-self::*[@data-date-field][1]');
}

export function dateFieldSegment(field: Locator, part: 'day' | 'month' | 'year'): Locator {
  return dateFieldRoot(field).locator(`[data-date-field-part="${part}"]`);
}

export async function fillDateField(field: Locator, isoDate: string): Promise<void> {
  const { year, month, day } = splitISODate(isoDate);

  await dateFieldSegment(field, 'day').fill(day);
  await dateFieldSegment(field, 'month').fill(month);
  await dateFieldSegment(field, 'year').fill(year);
  await dateFieldSegment(field, 'year').blur();
}

export async function clearDateField(field: Locator): Promise<void> {
  await dateFieldSegment(field, 'day').fill('');
  await dateFieldSegment(field, 'month').fill('');
  await dateFieldSegment(field, 'year').fill('');
  await dateFieldSegment(field, 'year').blur();
}
