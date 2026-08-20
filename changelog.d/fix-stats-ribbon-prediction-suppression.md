### Fixed

- **The Insights cycle stack stops claiming a fertile window once predictions are suppressed.** With
  "unpredictable cycle" switched on — or a pregnancy pause, or a cycle running past its own
  reference length — every other surface withholds the fertile window, the peak day and the
  ovulation phase, the calendar's past cycles included. The stack kept shading all three over its
  completed cycles on the strength of the "show historical phases" preference alone, so an account
  that had told the product its cycle math does not describe it still read an ovulation day off the
  stats page. Those cells now render blank there, and the recorded half of the stack is untouched:
  the rows, their observed lengths and their logged period days stay exactly as they were.
