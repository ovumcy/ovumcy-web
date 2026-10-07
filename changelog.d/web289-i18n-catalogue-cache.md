### Internal

- **A refused page no longer pays for the whole message catalogue.** Every page, a rate-limited
  refusal of the language switch included, copied both the default and the request's message
  catalogue into a fresh map before rendering. The merged catalogue for each language is now built
  once at start-up and shared, so a refusal costs what the page costs and no more.
