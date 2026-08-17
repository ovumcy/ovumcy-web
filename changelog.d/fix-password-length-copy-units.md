### Fixed

- **A password refused for being too long now says so, instead of being called weak.** The maximum
  is bcrypt's 72-byte input limit and is unchanged, but it was enforced through the same error as
  the composition rules and advertised as "8 to 72 characters" in every locale. A passphrase in a
  non-Latin script reaches the cap at roughly half that many characters — a 37-character Cyrillic
  passphrase is 73 bytes — so it was refused with a message it visibly satisfied, listing
  requirements it already met. Length now has its own error, carried through registration, password
  reset, the settings password forms and the JSON API: it names the one fact that helps, that
  letters from other alphabets and emoji count as several, and asks for a shorter password. A
  password that is both too long and missing a character class is reported as too long, because
  shortening is required either way. The weak-password message is back to character classes alone,
  and the hint under the field states the limit once.
