none

Dead-code removal with no user-visible effect: the header theme toggle's binder
and label sync, orphaned when the settings-interface radiogroup took over the
control, plus the theme-preference writer only that binder called; and the
manual-save finalizer's two byte-identical outcome branches collapse into one.
The four body dataset labels the removed binder read go with it, and so do the
two locale keys they rendered — `theme.switch_to_dark` and
`theme.switch_to_light`, whose only consumer was that binder. Leaving the
markup behind would have closed the orphan at the reader and opened it at the
writer: attributes rendered into every page for nobody. `theme.name.dark` and
`theme.name.light` stay — the settings-interface panel renders them.
