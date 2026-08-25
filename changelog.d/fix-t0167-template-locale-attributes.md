### Fixed

- **The segmented date field shows its day/month/year hints in the selected language.** The three
  boxes carried the English `DD`, `MM` and `YYYY` as literal markup, while the label above each box
  and its `aria-label` were both resolved from the catalogue — so on every date-entry surface a
  Russian, German or French account read an otherwise fully localized control with English
  abbreviations inside the boxes it was typing into. The hints now come from
  `date_field.day_placeholder`, `date_field.month_placeholder` and `date_field.year_placeholder`,
  defined in all six catalogues.

### Internal

- **A template barrier fails on human-readable copy typed into the markup.** It reports an `alt`,
  `title`, `placeholder` or `aria-*` value, and any visible text node, whose text carries letters
  and no template action — the one class neither existing locale sweep can see, since both start
  from a `t`/`tn` call and this copy never went through one. The reader works on the source regions
  the template parser attributed to text with every action masked out, so a value assembled from
  the catalogue leaves no letters behind to find. Two exceptions are declared with their reasons:
  the recovery-code mask on the forgot-password form, and the `°C` / `°F` unit symbols, which read
  the same in all six languages and are rendered beside their translated names.
