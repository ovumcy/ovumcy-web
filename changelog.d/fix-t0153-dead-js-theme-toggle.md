none

Dead-code removal with no user-visible effect: the header theme toggle's binder
and label sync, orphaned when the settings-interface radiogroup took over the
control, plus the theme-preference writer only that binder called; and the
manual-save finalizer's two byte-identical outcome branches collapse into one.
The body dataset labels the removed binder read are left in place — dropping
them orphans two locale keys, which is a catalogue change of its own.
