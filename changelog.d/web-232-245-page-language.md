### Fixed

- **A page no longer mixes two languages after a switch in another tab.** Content swapped into a
  page (a saved day, a refreshed panel) rendered in the language last chosen anywhere in the
  browser, so a French page could gain German sections. The page now reloads in the new language
  instead; the change that triggered it is still saved.
- **The export date summary is localized in every language and ends at the last entry.** On German
  and Italian pages it showed English month names, and in every language it ended at today rather
  than at the newest entry in the chosen range.
