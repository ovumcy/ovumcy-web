none

Tests only: a type-resolved guard fails when production Go writes to a map obtained from the i18n
manager's Messages or from currentMessages, which share one catalogue map per language.
