### Fixed

- **Primary buttons are readable again.** Login, "Update entry", "Save changes", "Change
  password", "Add custom symptom" and the other primary actions all render white bold text on
  the one `.btn-primary` component, whose warm gradient measured **2.23:1** at its dark stop and
  **1.63:1** at its light stop against that text — WCAG AA asks for 4.5:1 on text this size. The
  component now fills with a solid terracotta (5.77:1 in the light theme, 5.14:1 in the dark
  one), with hover, active and the three cycle-phase tints carried as tokens that clear the bar
  as well. Solid rather than a deeper gradient on purpose: text on a `background-image` is what
  automated contrast checks report as *incomplete* rather than as a failure, which is how this
  slipped through in the first place. A browser check now resolves the painted background — every
  stop of it, if it is ever a gradient again — and asserts 4.5:1 in both themes.
