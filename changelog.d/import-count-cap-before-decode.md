### Security

- **An oversized JSON restore is now refused before its entries are decoded.** The 20 000-entry cap
  on `/api/v1/imports/json` was checked only after the whole payload had already been unmarshalled
  into fully typed day records, so a crafted file packing far more than the cap into many small
  entries paid for decoding every one of them before the size refusal ever ran. The entry count is
  now read from a first, cheap pass over the raw JSON array, and the per-entry decode — the actual
  cost, one struct with several nested slices per entry — only runs once that count has already
  cleared the cap. The 16 MiB request body limit already bounded the wire size; this closes the gap
  between "fits under that ceiling" and "cheap to look at". Wire contract is unchanged: same 413
  and `error_detail.key: "import file too large"` for an over-cap file, same acceptance for anything
  under it.
