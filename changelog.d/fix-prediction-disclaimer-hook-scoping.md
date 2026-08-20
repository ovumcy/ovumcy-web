none

Test-only tightening: the settings egress regression now binds the medical
disclaimer to each surface that ships predicted dates off the instance, instead
of counting the wording across the whole page. Both hooks — the webhook reminder
and the calendar feed — are located in the parsed document, and each one's own
subtree must contain «not medical advice or a method of contraception». The old
assertion paired hook presence with a page-wide occurrence count, so emptying
the calendar-feed disclaimer while the webhook disclaimer stated the sentence
twice held the page total at two and kept the test green with one egress surface
carrying no qualifier at all. No production change: both templates already
render the shared key.
