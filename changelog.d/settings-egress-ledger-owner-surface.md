### Added

- **Settings now accounts for everything that can leave the instance, and for what each route can
  actually prove.** The webhook and calendar-feed sections have merged into one owner-only card,
  *Data leaving this instance*, and the two of them used to answer a weaker question than the one
  an owner was asking. Each said "configured" or "not configured", which is a fact about a stored
  value rather than about what the instance can do with it — a webhook endpoint the instance can no
  longer decrypt read as "configured", and a calendar link read as "active" whether or not it still
  resolved. Neither said a word about what the reminders and the feed actually carry.

  Each route now renders one state that the code can be shown wrong about. For webhook reminders
  those are: no endpoint stored; an endpoint stored that this instance can no longer read; an
  endpoint that names no host; this instance runs no reminder pass; delivery switched off; delivery
  on with no reminder type chosen; and delivery armed. Readability is decided before any toggle, so
  a broken application secret can no longer hide behind "delivery is off". For the calendar link,
  the word *active* is gone: what is knowable is which key signed the link, so the card says the
  link was issued under the key this instance runs now — the one case in which withdrawing it here
  cuts it off — or under a key it no longer runs, or before that was recorded at all.

  Beneath each state sits the one timestamp that route can prove, and nothing else. For webhook
  reminders that is the last delivery the receiving endpoint **accepted**, which is recorded after
  the fact and not when the send was decided. For the calendar link it is the one-time reveal of the
  subscribe URL being used. There is deliberately no count of fetches, no "last opened", no "in
  use" and no rotation history for the calendar feed: the feed's polls are not audited, by design,
  and no field exists that could answer a question about them.

  The card also lists, per route, exactly which fields leave — including the predicted date and the
  medical-safety sentence a reminder carries, and the event identifier an `.ics` entry builds from
  the reminder type and its date. The lists are pinned against what is really sent, so a change to
  either payload cannot quietly leave the page describing the old shape.

- **A webhook endpoint can be withdrawn on its own**, from a control beside its state, without
  submitting the whole settings form. Withdrawing it switches delivery off and clears the recorded
  delivery, while leaving your chosen reminder types and lead time exactly where they were. It works
  on an endpoint this instance can no longer read, which the previous route could not do: that path
  had to decrypt and re-save the URL, so the one row most in need of withdrawing was the one row it
  refused to act on.

- **`/privacy` links signed-in readers to that card.** The page itself stays unauthenticated and
  says nothing about how this instance is configured.

- **A webhook endpoint this instance can no longer read is no longer deleted by accident.** Leaving
  the URL field blank has always meant "keep the stored endpoint". When the stored value could not
  be decrypted — after the application secret is rotated — there was nothing to keep, and the save
  quietly stored an empty endpoint instead, but only when you were switching delivery off. So the
  destructive outcome arrived through the least alarming action on the page. The endpoint is now
  left exactly where it is: your reminder settings still save, delivery cannot be switched on over
  it, and removing it takes the withdraw control.

  Known state for this release: the six locales carry the new copy — the egress section and the new
  webhook error alike — and the five non-English ones are seeded with the English text pending
  translation.
