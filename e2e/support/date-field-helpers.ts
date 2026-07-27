import { type Locator, type Page } from '@playwright/test';

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

/**
 * The date exactly as the app's "display" format renders it for the given
 * locale, derived through `Intl` rather than pinned by a shape regex.
 *
 * A `/\w{3} \d{1,2}, \d{4}/` expectation is a per-language literal in disguise:
 * it encodes the EN-US ordering and separators, so it passes on the wrong date
 * and fails on any locale whose display format differs. Deriving the string
 * lets a prediction assertion check *which* date is shown, not merely that
 * something date-shaped is.
 */
export async function formatDisplayDate(
  page: Page,
  isoDate: string,
  intlLocale = 'en-US'
): Promise<string> {
  splitISODate(isoDate);
  return page.evaluate(
    ({ value, locale }) => {
      const date = new Date(`${value}T00:00:00`);
      return new Intl.DateTimeFormat(locale, {
        month: 'short',
        day: 'numeric',
        year: 'numeric',
      }).format(date);
    },
    { value: isoDate, locale: intlLocale }
  );
}

/**
 * Every display-formatted calendar date `text` contains, as ISO dates, in the
 * order they appear.
 *
 * Derived rather than pattern-matched: the browser formats a window of real
 * dates around today through the same `Intl` options the app's display format
 * uses, and reports which of those strings actually occur. That proves each
 * fragment is a real calendar date rendered in the app's display format —
 * something `/\w{3} \d{1,2}, \d{4}/` cannot do (it matches "Foo 99, 0000")
 * — without hardcoding EN-US ordering or separators anywhere.
 */
export async function displayDatesIn(
  page: Page,
  text: string,
  intlLocale = 'en-US',
  windowDays = 800
): Promise<string[]> {
  return page.evaluate(
    ({ haystack, locale, days }) => {
      const formatter = new Intl.DateTimeFormat(locale, {
        month: 'short',
        day: 'numeric',
        year: 'numeric',
      });
      const found: Array<{ at: number; iso: string }> = [];
      const anchor = new Date();
      anchor.setHours(0, 0, 0, 0);

      for (let offset = -days; offset <= days; offset += 1) {
        const candidate = new Date(anchor);
        candidate.setDate(candidate.getDate() + offset);
        const at = haystack.indexOf(formatter.format(candidate));
        if (at === -1) {
          continue;
        }
        const yyyy = candidate.getFullYear();
        const mm = String(candidate.getMonth() + 1).padStart(2, '0');
        const dd = String(candidate.getDate()).padStart(2, '0');
        found.push({ at, iso: `${yyyy}-${mm}-${dd}` });
      }

      return found.sort((a, b) => a.at - b.at).map((entry) => entry.iso);
    },
    { haystack: text, locale: intlLocale, days: windowDays }
  );
}
