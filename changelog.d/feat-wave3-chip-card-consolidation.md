### Changed

- **Chips and cards are now one component each.** Nine names for the same selectable token
  (`check-chip`, `choice-chip`, `mood-chip`, `role-chip`, `sex-chip`, `radio-tile` and their size
  variants) collapse into `chip` plus shape, size and state variants; ten card names collapse into
  `card` plus a quiet level, a hero, a density step, a grid-fit step, a modal size and two danger
  emphases. Both read the theme from tokens, so neither carries a dark-theme block of its own.
  Rendering is unchanged except where the duplication was hiding an inconsistency: the account chip
  in the navigation now shows the pointer cursor over its link, the "sex logged" summary chip reads
  as selected in the dark theme instead of only in the light one, and the pregnancy-test row centres
  its two options on a phone like the sibling day pickers already did.

### Internal

- **Four dead style declarations are gone, and the rules that replaced them state their intent.**
  A component rule that ties with a utility on the same element is decided by the bundle's emission
  order, so one of the two has always been dead: the confirmation dialog's message, the segmented
  date field's part labels and the two-factor code field each carried such a pair and now carry one
  declaration that says what it means. The dead compact-step font sizes left by the type scale in the
  calendar day panel and the symptom frequency row are removed as well.
