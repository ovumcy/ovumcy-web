---
name: feature-change
description: Plan and implement a new ovumcy feature using existing domain services, clean layering, and update governance hints when structure changes.
---

## Purpose

Help the user implement a feature in ovumcy by:
- mapping it to an existing domain (`auth`, `days/symptoms/cycle/viewer`, `settings/notifications`, `stats/dashboard/calendar`, `export`, `onboarding/setup`),
- changing services first and keeping `internal/api` transport-only,
- planning backend/frontend tests,
- suggesting small additions to AGENTS/SKILL docs when architecture/rituals become clearer.

## Workflow

1. Clarify feature and domain
   - Ask the user:
     - what the feature should do from the user’s point of view,
     - which pages or API endpoints are involved,
     - whether this affects only backend or also templates/JS.
   - Choose the primary domain from:
     - `auth/session/recovery/reset`,
     - `days/symptoms/cycle/viewer`,
     - `settings/notifications`,
     - `stats/dashboard/calendar`,
     - `export`,
     - `onboarding/setup`.

2. Map to services and API
   - Locate relevant services in `internal/services` for this domain.
   - Plan to:
     - extend or add service methods in `internal/services`,
     - then adapt `internal/api` handlers as thin transport (parsing, auth, CSRF, error mapping),
     - avoid adding business logic to handlers or templates.

3. Produce a small numbered plan
   - Before editing files, output a numbered list of small steps (5–10), each with:
     - which files in `internal/services` will change,
     - which files in `internal/api` will change,
     - whether `internal/templates` or `web/src/js` will change.
   - Mark any privacy/security-sensitive steps explicitly.

4. Implement services first
   - Modify or add service methods:
     - reuse existing domain types and error taxonomy,
     - return typed/domain errors to be mapped by the centralized API error mapping layer.
   - Add/extend unit tests in `internal/services` for happy paths, edge cases, and error conditions.

5. Adapt API and transport
   - Update `internal/api` handlers/helpers to:
     - call the new/extended service methods,
     - use centralized API error mapping (no inline status/message switches),
     - use shared content negotiation and error markup helpers (no ad-hoc header parsing or inline error HTML).
   - Add or update API regression tests to assert:
     - status codes + error keys,
     - HTMX vs full-page behavior,
     - redirects/flash where applicable.

6. Frontend impact (if any)
   - If templates or JS under `web/` change:
     - suggest `npm run lint:js` and `npm run build`,
     - list manual UI flows to test (by role: owner/partner, and pages affected).
   - Ask whether to keep or discard generated `web/static/*` diffs.
  - If the feature touches any “today” date logic, always check and reuse the request-local timezone propagation (ovumcy_tz cookie + X-Ovumcy-Timezone header) and add a UTC boundary regression test when relevant.
7. Final validation and governance suggestions
   - Recommend running:
     - targeted `go test` for affected services and API packages,
     - `go test ./...` if the feature is non-trivial.
   - Summarize:
     - what changed in services,
     - what changed in API,
     - what tests were added/updated.
   - At the end, explicitly propose 1–3 small, concrete suggestions for:
     - possible additions or tweaks to `AGENTS.md` (for example, new domain conventions, new cross-cutting rules you discovered),
     - or new/updated skills, if the feature exposed a new recurring pattern.
   - Do not modify AGENTS or SKILL files directly; only suggest text for the user to review.
   - For auth flows, explicitly check that redirect URLs on validation errors do not contain PII (email, tokens, error messages) in the query string or fragment.

## Constraints

- Always respect `AGENTS.md`:
  - no business logic in `internal/api`,
  - no direct DB access from handlers,
  - use centralized error mapping, negotiation, and markup helpers.
- Prefer extending existing services/domains over creating new ones.
- Never weaken privacy/security invariants; any security-sensitive behavior must be covered by tests.
