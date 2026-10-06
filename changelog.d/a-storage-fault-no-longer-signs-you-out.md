### Fixed

- **A storage fault no longer signs the owner out.** When the database could not be read while a
  request was checking its session — a locked or briefly unavailable database — the check took the
  failure for an account that no longer exists: it cleared the session cookie and answered as if
  nobody were signed in, so a form sent without JavaScript landed on the sign-in page with a "not
  signed in" notice. That notice could then surface on a later, unrelated sign-in. A fault now
  answers as a server error and leaves the session in place; an account that is really gone is
  still signed out as before.

- **A form sent by an account the web app no longer serves lands on the sign-in page.** Such a
  session is ended on the spot, but a form submitted without JavaScript answered with a page whose
  link back to the form only bounced to the sign-in page with no explanation. It now goes straight
  to the sign-in page, which says why.
