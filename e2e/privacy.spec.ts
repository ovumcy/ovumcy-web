import { expect, test } from '@playwright/test';
import { localeText } from './support/locale-helpers';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';

test.describe('Privacy page: copy and navigation', () => {
  test('public privacy page renders all sections with non-empty copy and a login back link', async ({
    page,
  }) => {
    await page.goto('/privacy');

    await expect(page.locator('[data-privacy-section="zero-collection"]')).toContainText(
      'Zero Data Collection',
    );
    await expect(page.locator('[data-privacy-section="your-data"]')).toContainText(
      'SQLite or PostgreSQL database on this server',
    );
    await expect(page.locator('[data-privacy-section="your-rights"]')).toContainText(
      'access, rectification, portability, restriction, and erasure',
    );
    await expect(page.locator('[data-privacy-section="retention"]')).toContainText(
      'Ovumcy does not delete your records on a schedule',
    );
    await expect(page.locator('[data-privacy-section="hidden-sections"]')).toContainText(
      'Hidden sections and exports',
    );
    await expect(page.locator('[data-privacy-section="predictions"]')).toContainText(
      'statistical summaries of your own logged days',
    );
    await expect(page.locator('[data-privacy-section="third-parties"]')).toBeVisible();
    await expect(page.locator('[data-privacy-section="open-source"]')).toBeVisible();

    await expect(page.locator('a[href="/login"]').first()).toBeVisible();
  });

  test('authenticated privacy page links breadcrumb and back link to /dashboard', async ({
    page,
  }) => {
    const credentials = createCredentials('privacy-auth-e2e');
    await registerOwnerViaUI(page, credentials);
    await expectInlineRegisterRecoveryStep(page);
    await readRecoveryCode(page);
    await continueFromRecoveryCode(page);
    await completeOnboardingIfPresent(page);

    await page.goto('/privacy');

    // Addressed by hook + href, with the label derived from the key the
    // template declares: `p.journal-muted.text-sm` was a styling-class path
    // that any layout tweak breaks, and 'Dashboard' is one of six spellings.
    const breadcrumb = page.locator('[data-privacy-breadcrumb] [data-privacy-breadcrumb-back]');
    await expect(breadcrumb).toBeVisible();
    await expect(breadcrumb).toHaveAttribute('href', '/dashboard');
    const breadcrumbKey = (await breadcrumb.getAttribute('data-breadcrumb-key')) ?? '';
    expect(breadcrumbKey, 'the breadcrumb must declare the label key it renders').not.toBe('');
    await expect(breadcrumb).toHaveText(localeText('en', breadcrumbKey));

    const bottomBackLink = page.locator('p.mobile-safe-target a[href="/dashboard"]');
    await expect(bottomBackLink).toBeVisible();
  });
});
