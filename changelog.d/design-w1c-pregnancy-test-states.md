### Changed

- **The pregnancy-test field stops treating "did not test" as an answer.** It used to render three
  identical segments with "None" filled at full weight, so every day in the app looked like a day
  with a test result on it. An untested day is now absent data: the field offers the two results
  only, nothing is filled, and it says so in words — "No result recorded for this day." The two
  results carry the same neutral tokens as every other day picker — no green, no red, no
  celebration, no emoji — and a selected one is confirmed by a filled ring as well as by its fill,
  so the state survives a colour-blind reading. Because "did not test" is no longer a segment
  anyone can tap, a saved result comes with an explicit secondary **Remove result** action next to
  it, which clears the day back to no result on the next save; a mis-tap is correctable before
  saving by choosing the other result, and after saving by removing it. Both day-entry surfaces —
  the dashboard journal and the calendar day editor — render the same control. Nothing else
  changes: the stored values stay `none`, `negative` and `positive`, a positive test still only
  pauses cycle predictions until a new period is logged (with the same neutral save message and
  the same medical guidance), and no test result of any kind rewrites the tracking goal, the
  predictions, or the tone of the product.
