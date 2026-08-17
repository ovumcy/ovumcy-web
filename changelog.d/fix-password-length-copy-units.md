### Fixed

- **Password length copy no longer promises characters where the limit counts bytes.** The maximum
  password length is bcrypt's 72-byte input limit and is unchanged, but every locale advertised it
  as "8 to 72 characters". A passphrase in a non-Latin script reaches the cap at roughly half that
  many characters — a 37-character Cyrillic passphrase is 73 bytes — so it was refused with a
  message it visibly satisfied and no action to take. The requirements hint and the weak-password
  error now state the minimum in characters and the maximum as 72 Latin characters, warning that
  letters from other alphabets and emoji count for several each.
