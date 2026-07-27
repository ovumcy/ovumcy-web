import { readFileSync } from 'node:fs';
import { join } from 'node:path';

/**
 * Single source of truth for every rendered-copy assertion in the e2e suite.
 *
 * Specs address rendered text through `data-*` hooks; when the copy itself is
 * the subject (language switching, a privacy scan that must cover all
 * languages) the expected strings are read from the locale catalogues the app
 * itself ships, never re-typed into a spec. A per-language literal branch rots
 * the moment a translation changes and silently misses a language added later —
 * reading `internal/i18n/locales/*.json` keeps both problems out of the suite.
 *
 * Loading happens in the Playwright worker process (Node), not in the browser.
 */

/**
 * The UI locales `i18n.SupportedLanguages()` ships. `TestLocaleKeysParity`
 * keeps the catalogues key-complete against each other, so every key resolved
 * here resolves in all six.
 */
export const SUPPORTED_LOCALES = ['en', 'ru', 'es', 'fr', 'de', 'it'] as const;

export type Locale = (typeof SUPPORTED_LOCALES)[number];

const LOCALES_DIR = join(__dirname, '..', '..', 'internal', 'i18n', 'locales');

const catalogues = new Map<Locale, Record<string, string>>();

function catalogue(locale: Locale): Record<string, string> {
  const cached = catalogues.get(locale);
  if (cached) {
    return cached;
  }

  const raw = readFileSync(join(LOCALES_DIR, `${locale}.json`), 'utf8');
  const loaded = JSON.parse(raw) as Record<string, string>;
  catalogues.set(locale, loaded);
  return loaded;
}

/**
 * The rendered string for one key in one locale.
 *
 * Throws on a missing or blank value rather than returning `''`: an empty
 * expectation would make `toContainText` pass against anything, turning a
 * renamed key into a green test.
 */
export function localeText(locale: Locale, key: string): string {
  const value = catalogue(locale)[key];
  if (typeof value !== 'string' || value.trim() === '') {
    throw new Error(`locale "${locale}" has no non-empty value for key "${key}"`);
  }
  return value;
}

/** The rendered string for one key in every supported locale, keyed by locale. */
export function localeTextByLocale(key: string): Record<Locale, string> {
  const table = {} as Record<Locale, string>;
  for (const locale of SUPPORTED_LOCALES) {
    table[locale] = localeText(locale, key);
  }
  return table;
}

/**
 * The rendered string for one key across every supported locale.
 *
 * Used by scans that must hold in all languages at once (the registration
 * enumeration check): sourcing the list here means a seventh locale is covered
 * the day it lands, instead of being silently skipped by a hardcoded list.
 */
export function everyLocaleText(key: string): string[] {
  return SUPPORTED_LOCALES.map((locale) => localeText(locale, key));
}

/**
 * Keys whose **English** copy matches `pattern`, sorted for stable output.
 *
 * English is the catalogue's source language, so a phrase-shaped rule only has
 * to be written once, in English. Callers expand the returned keys through
 * `everyLocaleText` to get the localized strings — which is what keeps a
 * multi-language forbidden-phrase scan free of per-language literals and
 * automatically complete when a new string or a new locale lands.
 */
export function localeKeysMatchingEnglish(pattern: RegExp): string[] {
  return Object.entries(catalogue('en'))
    .filter(([, value]) => typeof value === 'string' && pattern.test(value))
    .map(([key]) => key)
    .sort();
}
