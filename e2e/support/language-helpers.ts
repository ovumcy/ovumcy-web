import { expect, type Page } from '@playwright/test';

import { saveInterfaceSettingsForm } from './settings-interface-helpers';

export async function switchPublicLanguage(page: Page, code: string): Promise<void> {
  const form = page.locator('[data-language-switch-form]');
  const button = form.locator(`[data-language-switch-option="${code}"]`);

  await expect(form).toBeVisible();
  await expect(button).toBeVisible();

  await Promise.all([
    page.waitForNavigation({ waitUntil: 'domcontentloaded' }),
    button.click(),
  ]);

  await expect(button).toHaveAttribute('aria-pressed', 'true');
}

export async function saveSettingsLanguage(page: Page, code: string): Promise<void> {
  const form = page.locator('[data-settings-interface-form]');
  const option = form.locator(`[data-settings-interface-language-option="${code}"]`);

  await expect(form).toBeVisible();
  await expect(option).toBeVisible();

  if ((await option.getAttribute('data-selected')) !== 'true') {
    await option.locator('.chip-stack').click();
    // The radio change flips data-selected optimistically on the client, so the
    // assertion below can pass before the htmx PATCH persists the preference.
    // The shared helper waits on the save click's own request and its response,
    // so callers that navigate away next (e.g. straight to /calendar) don't race
    // an in-flight save that would otherwise drop the just-chosen language.
    // The success flash is not asserted here: those callers leave /settings
    // immediately and never land on the page that renders it.
    await saveInterfaceSettingsForm(page);
  }

  await expect(option).toHaveAttribute('data-selected', 'true');
}
