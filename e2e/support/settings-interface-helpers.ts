import { expect, type Page } from '@playwright/test';

/** The endpoint every settings interface save goes through. */
export const INTERFACE_SETTINGS_ENDPOINT = '/api/v1/users/current/interface';

/**
 * Presses the settings interface form's save button and returns only once the
 * server has answered that click's own PATCH.
 *
 * Waiting on the save button's state does not prove that. The shared htmx busy
 * handler only touches `form[data-save-feedback] [data-save-button]`, and the
 * interface form is neither, so nothing disables this button while its PATCH is
 * outstanding — what re-disables it is the reload the endpoint's `HX-Redirect`
 * triggers. That makes "disabled again" a proxy for "some page loaded", not for
 * "the account took the save". Nor do the form's own attributes help: the radio
 * change flips `data-selected` optimistically, and the theme is written to
 * `localStorage` synchronously in the submit listener, both before any network
 * I/O.
 *
 * So the wait binds to the click's OWN request and to that request's response,
 * which is the only wait that outlives the PATCH and cannot be satisfied by a
 * navigation the caller did not ask for.
 *
 * `expectSuccessFlash` adds the server-backed half: the handler puts the notice
 * in a flash cookie and answers with a redirect, so the notice is rendered by
 * `/settings` itself and no client state can fabricate it. It is opt-in because
 * a caller that only needs the save not to be raced — one that navigates
 * elsewhere straight after — never lands on the page that renders it.
 */
export async function saveInterfaceSettingsForm(
  page: Page,
  options: { expectSuccessFlash?: boolean } = {},
): Promise<void> {
  const form = page.locator('[data-settings-interface-form]');
  const [saveRequest] = await Promise.all([
    page.waitForRequest(
      (request) =>
        request.method() === 'PATCH' && request.url().includes(INTERFACE_SETTINGS_ENDPOINT),
    ),
    form.locator('[data-settings-interface-save]').click(),
  ]);

  const saveResponse = await saveRequest.response();
  expect(
    saveResponse?.ok(),
    `the interface PATCH answered ${String(saveResponse?.status())}, so nothing was saved`,
  ).toBe(true);

  if (options.expectSuccessFlash) {
    await expect(
      page.locator(
        '[data-flash-key="settings.success.interface_updated"][data-flash-status="success"]',
      ),
    ).toBeVisible();
  }
}
