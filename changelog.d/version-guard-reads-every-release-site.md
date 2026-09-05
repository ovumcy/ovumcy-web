none

Internal only: the readmeversion guard now walks every docker-compose*.y*ml
file's `${OVUMCY_IMAGE:-...}` fallback (including the root docker-compose.yml,
which it previously never read) and the README's mutable-tag sentence, both
against the release tag README.md asserts. No user-visible behavior changes.
