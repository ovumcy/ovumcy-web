### Fixed

- **A refused form no longer offers a language switch that is refused in turn.** When a form sent
  without JavaScript was turned away by the cross-site check or by a rate limit, the refusal page
  showed the language buttons in its header, but with no proof of origin left to send: every button
  led to the same refusal again. The page now offers the way back alone; a refusal the form reached
  past those checks keeps its working switch.
