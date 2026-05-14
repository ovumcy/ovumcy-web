/**
 * Screenshot helper — injects an existing session into Playwright and saves:
 *   docs/screenshots/register.jpg
 *   docs/screenshots/dashboard.jpg
 *   docs/screenshots/calendar.jpg
 *   docs/screenshots/settings-export.jpg
 *   docs/screenshots/dark-theme.jpg
 *   docs/screenshots/install-prompt.png
 *
 * Usage: node scripts/take-screenshots.mjs [base-url]
 * Default base-url: http://127.0.0.1:9191
 *
 * Requires an already-registered+onboarded account.
 * Cookies are passed via AUTH_COOKIE env var or hardcoded below.
 */

import { chromium } from 'playwright';
import path from 'path';
import { fileURLToPath } from 'url';
import { execSync } from 'child_process';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const BASE = process.argv[2] ?? 'http://127.0.0.1:9191';
const OUT  = path.resolve(__dirname, '..', 'docs', 'screenshots');

// ── Auth cookies from the working curl session ───────────────────────────────
// These are read at runtime; override via env vars if needed.
const AUTH_COOKIE = process.env.AUTH_COOKIE ??
  'v2.b2d2sw3ZYfZC0WHLF_DQOLdn3d0ZXZg0lyK5f0w350b4XWBmXjSTYjlMRKwIQ307EU3Laj1qhnq-4PnZx4GWmnytaaBeDIVHUY5RCTSsvaHGD7FTuzgpTnXlsy6mgdMlAOWpV5vZ31IUkZR1H7M-g7qFS5-Lh7zcjUC_GKN0K2OTAKbhJdW5HNZgoWWZ4EZD-NEQqMZnRwe-vJvagMTLMqtzQEAKVgmUTAAUvODfGlyy9yc8fI7oSA_5nEyI5wmgkK2WlBCCnV9QC4HQBIMZH1TPxFXT25ftFDJvtz7u5CsZKKRgm1S4Kdp21xaTJ7Va7noY8vGVzyOIj1E';

const CSRF_COOKIE = process.env.CSRF_COOKIE ?? '4f988926-f133-4f71-9a44-ae5a56508e45';

const SESSION_COOKIES = [
  { name: 'ovumcy_auth',  value: AUTH_COOKIE,  domain: '127.0.0.1', path: '/', httpOnly: true,  sameSite: 'Lax' },
  { name: 'ovumcy_csrf',  value: CSRF_COOKIE,  domain: '127.0.0.1', path: '/', httpOnly: true,  sameSite: 'Lax' },
  { name: 'ovumcy_tz',    value: 'Europe/London', domain: '127.0.0.1', path: '/', httpOnly: false, sameSite: 'Lax' },
  { name: 'lang',         value: 'en',          domain: '127.0.0.1', path: '/', httpOnly: false, sameSite: 'Lax' },
];

async function jpegShot(page, file) {
  const dest = path.join(OUT, file);
  await page.screenshot({ path: dest, type: 'jpeg', quality: 90, fullPage: false });
  console.log('✓', file);
}
async function pngShot(page, file) {
  const dest = path.join(OUT, file);
  await page.screenshot({ path: dest, type: 'png', fullPage: false });
  console.log('✓', file);
}

async function goto(page, url) {
  await page.goto(url);
  await page.waitForLoadState('networkidle');
  console.log('  at:', page.url());
}

async function main() {
  const browser = await chromium.launch({ headless: true });

  // ── 1. register.jpg — fresh context, no auth, English ──────────────────────
  {
    const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 }, locale: 'en-US' });
    await ctx.addCookies([{ name: 'lang', value: 'en', domain: '127.0.0.1', path: '/' }]);
    const page = await ctx.newPage();
    await goto(page, `${BASE}/register`);
    // switch to English if not already (the lang cookie may not be applied yet)
    const currentLang = await page.$('[data-lang="en"].active, button[data-lang="en"]');
    if (!currentLang) {
      // click EN button
      const en = await page.$('button:has-text("EN"), a:has-text("EN"), [data-lang="en"]');
      if (en) { await en.click(); await page.waitForLoadState('networkidle'); }
    }
    await page.waitForTimeout(400);
    await jpegShot(page, 'register.jpg');
    await ctx.close();
  }

  // ── Desktop authenticated context ──────────────────────────────────────────
  const ctx = await browser.newContext({
    viewport:  { width: 1280, height: 800 },
    locale:    'en-US',
  });
  await ctx.addCookies(SESSION_COOKIES);
  const page = await ctx.newPage();

  // ── 2. dashboard.jpg ───────────────────────────────────────────────────────
  await goto(page, `${BASE}/dashboard`);
  await page.waitForTimeout(600);
  await jpegShot(page, 'dashboard.jpg');

  // ── 3. calendar.jpg ────────────────────────────────────────────────────────
  await goto(page, `${BASE}/calendar`);
  await page.waitForTimeout(600);
  await jpegShot(page, 'calendar.jpg');

  // ── 4. settings-export.jpg ─────────────────────────────────────────────────
  await goto(page, `${BASE}/settings`);
  await page.waitForTimeout(400);

  // Scroll to the data / export section (id="settings-data", data-export-section)
  await page.evaluate(() => {
    const el = document.getElementById('settings-data') ?? document.querySelector('[data-export-section]');
    if (el) el.scrollIntoView({ behavior: 'instant', block: 'start' });
  });
  await page.waitForTimeout(400);
  await jpegShot(page, 'settings-export.jpg');

  // ── 5. dark-theme.jpg ──────────────────────────────────────────────────────
  // Set localStorage before reload so theme-bootstrap.js picks it up on page init.
  await goto(page, `${BASE}/dashboard`);
  await page.evaluate(() => {
    try { localStorage.setItem('ovumcy_theme', 'dark'); } catch(_) {}
  });
  await page.reload({ waitUntil: 'networkidle' });
  await page.waitForTimeout(500);
  await jpegShot(page, 'dark-theme.jpg');

  await ctx.close();

  // ── 6. install-prompt.png — mobile viewport ────────────────────────────────
  const mobileCtx = await browser.newContext({
    viewport:  { width: 390, height: 844 },
    userAgent: 'Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1',
    locale:    'en-US',
    isMobile:  true,
    hasTouch:  true,
  });
  // Inject session into mobile context too
  await mobileCtx.addCookies(SESSION_COOKIES);
  const mPage = await mobileCtx.newPage();

  await goto(mPage, `${BASE}/dashboard`);
  await mPage.waitForTimeout(600);

  // Try to surface the PWA install banner if the app has one
  await mPage.evaluate(() => {
    const e = new Event('beforeinstallprompt', { bubbles: true });
    Object.assign(e, { platforms: ['web'], prompt: () => Promise.resolve(), userChoice: Promise.resolve({ outcome: 'accepted' }) });
    window.dispatchEvent(e);
  });
  await mPage.waitForTimeout(800);

  await pngShot(mPage, 'install-prompt.png');
  await mobileCtx.close();

  await browser.close();
  console.log('\nAll screenshots saved to', OUT);
}

main().catch(err => { console.error(err); process.exit(1); });
