### Fixed

- **Unchecking a period start no longer erases a flow you logged by hand on a later day.** With
  automatic period fill on, unchecking the start day also clears the following days the fill had
  created, stopping at the first day carrying anything you entered. A flow (including spotting)
  was not counted as something you entered, so a hand-logged heavy day right after the start was
  cleared with it. A flow now stops the clearing unless it is the very flow the fill copied from
  the start day.
