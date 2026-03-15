import { expect, type Locator, type Page } from '@playwright/test';

type NoteScope = Locator | Page;

export async function ensureNotesFieldVisible(
  scope: NoteScope,
  fieldSelector: string
): Promise<Locator> {
  const disclosure = scope.locator('details.note-disclosure').first();
  const field = scope.locator(fieldSelector).first();

  if ((await disclosure.count()) > 0) {
    if (!(await disclosure.evaluate((node) => node.hasAttribute('open')))) {
      await disclosure.locator('summary').click();
    }
    await expect(disclosure).toHaveAttribute('open', '');
  }

  await expect(field).toHaveCount(1);
  await expect(field).toBeVisible();
  return field;
}
