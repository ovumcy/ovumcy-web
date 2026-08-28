### Internal

- **The export doc described the CSV `Cycle factors` cell as machine keys.** It emits the same
  human-readable labels the app shows — `Stress`, `Sleep disruption` — joined with `"; "`, not
  the lowercase enum strings the JSON export carries; a consumer coding against the doc
  mis-parsed every non-empty cell. The separator nit is fixed on the `Other` column too, and
  the JSON `cycle_factors` field no longer calls its five-key catalog "free-form".
- **The documented `Content-Disposition` header now matches the one the server sends** — the
  filename is unquoted on both the JSON and the CSV response.
- **`docs/cycle-prediction.md` documents the first-cycle floor.** The document claimed to
  describe the estimates "in full" while the fertility half — ovulation date, fertile window,
  peak band, feed events, webhook reminder, dashboard banner — has been withheld until one
  cycle completes since the floor landed. The math and every worked example were and remain
  correct; what was missing is the gate that decides whether those numbers reach a surface.
- **`docs/hero-demo.md` names the whole pack and the scripts that generate it.** `docs/demo.gif`
  (with `demo.mp4` behind it) is the README's hero and the doc never mentioned it, nor
  `scripts/take-screenshots.mjs` / `scripts/record-demo.mjs`, so a reader regenerating the pack
  from the Capture Checklist would leave the one moving asset stale and unreviewed for PII.
