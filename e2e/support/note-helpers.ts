import { expect, type Locator, type Page } from '@playwright/test';

type NoteScope = Locator | Page;

export async function ensureNotesFieldVisible(
  scope: NoteScope,
  fieldSelector: string
): Promise<Locator> {
  const field = scope.locator(fieldSelector).first();
  await expect(field).toHaveCount(1);
  await expect(scope.locator('details.note-disclosure')).toHaveCount(0);
  await expect(field).toBeVisible();
  return field;
}
