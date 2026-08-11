### Fixed

- **A day entry that fails to save now says so, and offers to send it again.** On a self-hosted
  instance the server is routinely unreachable — the owner is off the home network — and a save that
  died in flight left the calendar day editor silent: the button un-pressed itself and nothing else
  changed, which is indistinguishable from a save that worked. The editor now answers a failed save
  in its existing `aria-live="polite"` status region with a neutral notice and a retry control that
  resubmits the same form. The form survives in the DOM; nothing is persisted client-side.

### Changed

- **A failed day-entry save reads as a connection problem, not as a health warning.** The failure
  notice uses a neutral surface instead of the red error block a health screen shares with real
  findings, and carries calm copy in all six UI languages ("Couldn't save — check your connection.
  Your entry is still here."). When the server did answer with its own message, that message is
  shown instead, on the same neutral surface and with the same retry.
