# Data Export Format

Ovumcy supports three export endpoints under `Settings → Export`:

- `GET /api/v1/exports/json` — full per-day records as JSON.
- `GET /api/v1/exports/csv` — full per-day records as CSV.
- `GET /api/v1/exports/summary` — small JSON summary used by the Settings UI.

All three accept an optional date range via query parameters `from` and `to` (`YYYY-MM-DD`, in the user's timezone). Omitting them exports everything.

Each export endpoint requires the `owner` role and a valid auth session. CSRF is not required — these are GET reads and the auth cookie alone is sufficient.

## JSON Export

```http
GET /api/v1/exports/json?from=2026-01-01&to=2026-05-31
Cookie: ovumcy_auth=...
```

Response headers: `Content-Disposition: attachment; filename="ovumcy-export-<timestamp>.json"`, `Content-Type: application/json`.

Response body:

```json
{
  "exported_at": "2026-05-17T14:32:01+03:00",
  "entries": [
    {
      "date": "2026-05-17",
      "period": true,
      "cycle_start": false,
      "is_uncertain": false,
      "flow": "medium",
      "mood_rating": 3,
      "sex_activity": "protected",
      "bbt": 36.7,
      "cervical_mucus": "creamy",
      "pregnancy_test": "negative",
      "cycle_factors": ["stress", "travel"],
      "symptoms": {
        "cramps": true,
        "headache": false,
        "acne": false,
        "mood": false,
        "bloating": true,
        "fatigue": true,
        "breast_tenderness": false,
        "back_pain": false,
        "nausea": false,
        "spotting": false,
        "irritability": false,
        "insomnia": false,
        "food_cravings": false,
        "diarrhea": false,
        "constipation": false
      },
      "other_symptoms": ["my-custom-symptom"],
      "notes": "felt tired all afternoon"
    }
  ]
}
```

Field semantics:

| Field | Type | Notes |
| --- | --- | --- |
| `exported_at` | RFC 3339 string | Server time at export, in the user's timezone. |
| `entries` | array | One entry per logged day. Days with no data are not exported. |
| `date` | `YYYY-MM-DD` string | Calendar day in the user's timezone. |
| `period` | boolean | Whether the day is marked as a period day. |
| `flow` | string | One of `none`, `spotting`, `light`, `medium`, `heavy`. |
| `mood_rating` | integer | User-selected mood scale. Zero means unset. |
| `sex_activity` | string | One of `none`, `protected`, `unprotected`. |
| `bbt` | float | Basal body temperature in the unit selected per account (°C or °F). Emitted only when measured; the key is absent on unmeasured days. On import, an absent key, an explicit `null`, or a legacy `0` are all read as "not measured". |
| `cervical_mucus` | string | One of `none`, `dry`, `moist`, `creamy`, `eggwhite`. |
| `pregnancy_test` | string | One of `none`, `negative`, `positive`. |
| `cycle_factors` | array of strings | Free-form factor keys recorded that day (e.g. `stress`, `travel`, `illness`). |
| `symptoms` | object of booleans | Flags for the 15 built-in symptoms. Always present, even when all false. |
| `other_symptoms` | array of strings | Names of owner-managed custom symptoms recorded that day. |
| `notes` | string | Free-text note. |
| `cycle_start` | boolean | Whether the day is the manually marked start of a cycle. Owner-only. |
| `is_uncertain` | boolean | Whether the owner flagged this day's data as uncertain. Owner-only. |

The `symptoms` object always contains the same 15 keys, in this order: `cramps`, `headache`, `acne`, `mood`, `bloating`, `fatigue`, `breast_tenderness`, `back_pain`, `nausea`, `spotting`, `irritability`, `insomnia`, `food_cravings`, `diarrhea`, `constipation`. Any other symptom configured by the owner appears in `other_symptoms` as a free-text name.

## CSV Export

```http
GET /api/v1/exports/csv?from=2026-01-01&to=2026-05-31
Cookie: ovumcy_auth=...
```

Response headers: `Content-Disposition: attachment; filename="ovumcy-export-<timestamp>.csv"`, `Content-Type: text/csv`.

Columns (in order, single header row):

```
Date, Period, Flow, Mood rating, Sex activity, BBT (C), Cervical mucus,
Cramps, Headache, Acne, Mood, Bloating, Fatigue, Breast tenderness,
Back pain, Nausea, Spotting, Irritability, Insomnia, Food cravings,
Diarrhea, Constipation, Cycle factors, Other, Notes, Pregnancy test,
Cycle start, Uncertain
```

Cell semantics:

- `Date` is `YYYY-MM-DD` in the user's timezone.
- `Period`, and the 15 symptom columns, are `true`/`false`.
- `Flow`, `Sex activity`, `Cervical mucus` carry the same string vocabulary as the JSON export.
- `BBT (C)` is the float value in the unit selected on the account; the cell is empty on days with no measurement. The header keeps the literal text `BBT (C)` for stability and does not change to `BBT (F)` for Fahrenheit accounts. Operators reading the file should consult the account's `temperature_unit` setting (or the source UI) to interpret the unit.
- `Cycle factors` is a `;`-separated list of factor keys; empty when none were recorded.
- `Other` is a `;`-separated list of owner-managed custom symptom names; empty when none.
- `Notes` is the free-text note; the CSV writer quotes the cell as needed.
- `Pregnancy test` is one of `none`, `negative`, `positive`. It was introduced after the 1.1.1 layout and appended after the original columns so existing column positions stay stable, per the stability rule below.
- `Cycle start` and `Uncertain` are `Yes`/`No`. They are the last two columns, appended after `Pregnancy test` so existing column positions stay stable, per the stability rule below. `Cycle start` marks the manually flagged start of a cycle; `Uncertain` marks a day the owner flagged as uncertain. Both are owner-only.

## Summary Export

```http
GET /api/v1/exports/summary?from=2026-01-01&to=2026-05-31
Cookie: ovumcy_auth=...
```

Response is JSON (not a file download), used by the Settings UI before showing the full export buttons:

```json
{
  "total_entries": 142,
  "has_data": true,
  "date_from": "2025-09-01",
  "date_to": "2026-05-17"
}
```

`date_from`/`date_to` are absent or empty strings when the range is unbounded.

## Import (restore)

`POST /api/v1/imports/json` restores a prior JSON export back into the current account. It is the inverse of `GET /api/v1/exports/json` and consumes the exact `{ "exported_at": ..., "entries": [...] }` shape documented above (`exported_at` is ignored on import).

Request: `Content-Type: application/json`, body = the raw export file. Requires the `owner` role, a valid auth session, and a CSRF token (`X-CSRF-Token` header or `csrf_token` form field) — it is state-mutating, unlike the GET exports.

The restore is **additive**: each entry creates its day only if the account does not already have that calendar day. Existing days are never overwritten or deleted, so no password re-authentication is required (contrast clear-data / delete-account), and re-importing the same file is safe and idempotent.

Every field is re-validated and sanitized server-side (the file is untrusted input): unknown enum values fall back to their neutral default, `notes` is length-capped, invalid cycle-factor keys are dropped, and `cycle_start`/`is_uncertain` are cleared on non-period days. Built-in symptom flags map back to the owner's own catalog; names in `other_symptoms` are matched to existing symptoms or created as custom symptoms. All writes are scoped to the session owner.

Response `200`:

```json
{ "ok": true, "added": 128, "skipped": 14, "rejected": 0 }
```

- `added` — days created.
- `skipped` — days that already existed and were left untouched.
- `rejected` — malformed or duplicate day records dropped without writing.

Error responses use stable, PII-free keys: `400 invalid import file` (not valid JSON), `413 import file too large`, `500 failed to import data`.

Importing from other trackers (e.g. Drip) is out of scope for this endpoint — see [issue #116](https://github.com/ovumcy/ovumcy-web/issues/116).

## Stability

The JSON entry shape and CSV columns are stable across Ovumcy patch and minor releases. Breaking changes (renaming or removing a field, changing the value vocabulary, reordering CSV columns) ship in a major release with a migration note in `CHANGELOG.md`. Adding a new field to the JSON entry or a new column at the end of the CSV is non-breaking and does not trigger a major bump.

The wrapping JSON shape (`exported_at` / `entries`) follows the same rule.
