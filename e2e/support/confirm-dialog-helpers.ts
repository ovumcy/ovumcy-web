import { expect, type Locator, type Page, type Request } from '@playwright/test';

/**
 * Helpers for the custom confirm dialog that gates destructive htmx controls.
 *
 * The dialog is CSP-safe markup in the base layout driven by an `htmx:confirm`
 * listener: htmx withholds the request until the promise the listener returns
 * resolves, so Cancel must leave the page untouched. Proving that needs two
 * things a plain `expect(...).toBeVisible()` cannot give — a record of what the
 * page actually put on the wire, and the captions the dialog really rendered.
 */

export type ConfirmDialogCaptions = {
  accept: string;
  cancel: string;
};

/**
 * Asserts the open dialog renders the captions the surface declared, as visible
 * button text.
 *
 * The accept caption is per-control (`data-confirm-accept` on the element that
 * carries `hx-confirm`); the cancel caption is layout-wide
 * (`data-confirm-cancel` on `<body>`). Backend coverage pins that both are
 * declared — this pins that they survive the trip into the dialog instead of
 * silently falling back to the layout default or to a hardcoded English word.
 * Both declarations are asserted non-empty first so an empty attribute cannot
 * make the comparison pass against empty rendered text.
 */
export async function expectConfirmDialogCaptions(
  page: Page,
  trigger: Locator
): Promise<ConfirmDialogCaptions> {
  await expect(page.locator('#confirm-modal')).toBeVisible();

  const accept = ((await trigger.getAttribute('data-confirm-accept')) ?? '').trim();
  expect(accept, 'the confirm-gated control must declare data-confirm-accept').not.toBe('');

  const cancel = ((await page.locator('body').getAttribute('data-confirm-cancel')) ?? '').trim();
  expect(cancel, 'the layout must declare data-confirm-cancel').not.toBe('');

  const acceptButton = page.locator('#confirm-modal-accept');
  const cancelButton = page.locator('#confirm-modal-cancel');
  await expect(acceptButton).toBeVisible();
  await expect(cancelButton).toBeVisible();
  await expect(acceptButton).toHaveText(accept);
  await expect(cancelButton).toHaveText(cancel);

  return { accept, cancel };
}

/** Dismisses the open dialog and waits for it to close. */
export async function cancelConfirmDialog(page: Page): Promise<void> {
  await expect(page.locator('#confirm-modal')).toBeVisible();
  await page.locator('#confirm-modal-cancel').click();
  await expect(page.locator('#confirm-modal')).toBeHidden();
}

/** Confirms the open dialog, which is what releases the withheld htmx request. */
export async function acceptConfirmDialog(page: Page): Promise<void> {
  await expect(page.locator('#confirm-modal')).toBeVisible();
  await page.locator('#confirm-modal-accept').click();
}

/**
 * Records every non-GET request the page issues to a matching path while
 * `action` runs. Method is part of the filter because the day endpoints serve
 * saves and deletes under the same pathname, and reads (reloads, htmx partial
 * fetches) must not be mistaken for a mutation that escaped the dialog.
 */
export async function mutatingRequestsDuring(
  page: Page,
  matchesPath: (pathname: string) => boolean,
  action: () => Promise<void>
): Promise<string[]> {
  const seen: string[] = [];
  const listener = (request: Request) => {
    if (request.method() === 'GET') {
      return;
    }
    if (matchesPath(new URL(request.url()).pathname)) {
      seen.push(`${request.method()} ${request.url()}`);
    }
  };

  page.on('request', listener);
  try {
    await action();
  } finally {
    page.off('request', listener);
  }
  return seen;
}
