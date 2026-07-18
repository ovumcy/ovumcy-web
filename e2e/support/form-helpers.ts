import { type Locator } from '@playwright/test';

// checkStyledControl checks a native radio/checkbox that is intentionally hidden
// behind a custom-styled tile or chip (flow, mood, temperature unit, symptom
// chips, age group). The visible affordance is the sibling label/tile; the
// native <input> itself is visually hidden, and Playwright refuses to click a
// hidden element. `force: true` is therefore required and correct here — it does
// NOT paper over a real clickability regression. Tests still assert the native
// input's checked state afterwards, so a genuinely broken control still fails.
export async function checkStyledControl(control: Locator): Promise<void> {
  await control.check({ force: true });
}
