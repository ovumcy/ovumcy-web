### Changed

- **The medical-safety disclaimer is one sentence, written once.** The dashboard,
  Insights, the calendar, the calendar-feed panels and the webhook-reminders
  section used to spell "estimates, not medical advice or a method of
  contraception" out in three separate catalogue entries that had drifted apart.
  They now all render the same string, and every surface that showed the
  qualifier still shows it — the webhook section carries it as its own line
  instead of hiding it at the end of a paragraph.
- **The login screen uses one verb.** The heading, the submit button and the SSO
  button said "Log in", "Login" and "Sign in" in English, and mixed a noun with a
  verb in Spanish and French. All three now read as the same action in every
  language.
- **The dashboard says which dates its next-period range covers.** The status
  line rendered the predicted start window and the predicted period days with the
  same bare "date — date" string, so the two could not be told apart. Each now
  names itself, in the same words the calendar legend uses.
- **Warnings and save messages carry no emoji.** The future-date notice, the
  short-gap cycle-start confirmation and the self-care save message had a glyph
  baked into the translated sentence; the future-date notice now shows the
  regular alert icon and the other two are plain sentences.

### Removed

- **The "your prediction shows a range" explainer.** The prediction has rendered
  as a range for a while and the range now names itself, so the sentence under it
  only restated the surface. Owners with an irregular cycle keep their own
  explainer.

### Internal

- Twenty-five locale keys left unrendered by the settings density pass are
  deleted from all six catalogues.
