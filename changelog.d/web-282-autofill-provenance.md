### Fixed

- **Un-ticking a period start clears exactly the days auto-fill wrote.** Each day now records
  whether the period auto-fill wrote it (migration 042 adds a nullable `daily_logs.auto_filled`).
  Un-ticking the start used to guess this from flow, so a fill whose start was later edited to a
  different flow stayed behind, and a day ticked by hand with the start's flow was erased. Now
  fill-written days are cleared whatever the start's flow became, and any day saved by hand is kept,
  even when saved unchanged. Days written before this release carry no mark and are still judged by
  flow. Export does not carry the mark, and every imported day counts as entered by hand.
