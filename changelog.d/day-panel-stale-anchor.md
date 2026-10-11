### Internal

- **The day panel's projection check searches the out-of-date suppression reason among the strings
  that must name its words.** The word "stale" was found only in an unrelated settings error key,
  so rewording that key would have failed the check for a reason it does not test. The
  `cycle_data_stale` reason is now one of the searched strings; the check is satisfied by any
  string that names the word, so the reason is an additional anchor, not the only one.
