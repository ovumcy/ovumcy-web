### Fixed

- **A cycle start logged on the day of a positive pregnancy test now resumes tracking.** The pause
  notice tells the owner to log a new period to resume, but a start marked on the test day itself
  was compared strictly after the test and left predictions paused on every surface. The start now
  lifts the pause when it falls on or after the test day (and not after the owner's today, as
  before); a start the day before the test still leaves it paused.
