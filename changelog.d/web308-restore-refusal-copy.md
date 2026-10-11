### Fixed

- **A refused settings restore now says why.** The restore page looked for the refusal key where the
  server never puts it, so every refusal showed "Restore failed. Please try again.". It now reads the
  key from the error envelope and shows the message for an invalid file, a file that is too large
  (including one over the request size cap), and a restore refused because another account is now
  signed in, which gets its own message in all six languages. Any other refusal keeps the generic
  message.
