# Hero Demo Asset Pack

This document defines the privacy-safe hero demo pack for Ovumcy.

The goal is to keep one reusable asset set for README screenshots, release notes, and short walkthrough clips without ever relying on a live public instance.

The pack is the six stills under `docs/screenshots/` plus the recorded first-run clip `docs/demo.gif` (with `docs/demo.mp4` as its source copy), which the README embeds as its hero. Both halves are generated: `scripts/take-screenshots.mjs` writes the six stills, `scripts/record-demo.mjs` writes the GIF and the MP4. Regenerate through those scripts — the Capture Checklist below is the manual description of what they do and the privacy review they cannot do for you.

## Core Flow

Use these assets in this order when building a short walkthrough or landing-page demo:

1. Register: [`docs/screenshots/register.jpg`](./screenshots/register.jpg)
2. Dashboard: [`docs/screenshots/dashboard.jpg`](./screenshots/dashboard.jpg)
3. Calendar: [`docs/screenshots/calendar.jpg`](./screenshots/calendar.jpg)
4. Settings and export: [`docs/screenshots/settings-export.jpg`](./screenshots/settings-export.jpg)
5. Mobile install prompt: [`docs/screenshots/install-prompt.png`](./screenshots/install-prompt.png)
6. Dark theme: [`docs/screenshots/dark-theme.jpg`](./screenshots/dark-theme.jpg)

This sequence matches the current product story: create an account, log today quickly, review the month, export or tune settings, install the app on a phone home screen, then show the dark theme option.

## Privacy Rules

- Never capture a live public deployment.
- Use a local or otherwise private self-hosted instance only.
- Use seeded sample data and generic identity values.
- Avoid real email addresses, real notes, and any personal health history in captures.
- Keep the install-prompt asset synthetic and local; it can be driven by the same browser event simulation used in `e2e/pwa-install.spec.ts`.

## Asset Guidance

The still half of the pack is intentionally static-first:

- `register.jpg` covers the first-run entry point.
- `dashboard.jpg` covers the primary daily logging surface.
- `calendar.jpg` covers month review and cycle context.
- `settings-export.jpg` covers data ownership and export.
- `install-prompt.png` covers phone install CTA behavior.
- `dark-theme.jpg` covers the dark theme option.
- `demo.gif` covers the first-run flow in motion; it is the one recorded asset, and every privacy rule in this document applies to it exactly as to a still.

For short release clips or social cuts, prefer stitching these assets together over recording a live server session unless a release specifically needs motion.

## Capture Checklist

When regenerating the pack:

1. Start a local instance with a private demo account and seeded sample data.
2. Capture the four authenticated surfaces from that local instance (`node scripts/take-screenshots.mjs`, which needs an already-onboarded account and its cookie).
3. Capture the mobile install prompt on `/login` with a mobile viewport and a synthetic `beforeinstallprompt` event.
4. Capture the dark theme surface with the dark theme option enabled.
5. Re-record the clip if the first-run flow changed (`node scripts/record-demo.mjs`, which needs ffmpeg and a built app).
6. Review every frame for accidental PII before publishing — the GIF frame by frame, not only the stills.
