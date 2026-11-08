### Fixed

- **Security and self-hosting documents now say what the code does.** Corrected texts:
  flash cookies carry a five-minute server-side bound; a stated `Sec-Fetch-Site: same-site`
  is refused on the one-time reveal routes; the published image is scanned after its by-digest
  push and before signing or tagging; `SECRET_KEY` startup checks cover length and known
  placeholders, not randomness; a webhook revocation committed after the delivery claim is won
  does not stop that one send; the audit stream outlives account erasure; the Docker Hub mirror
  carries no public tag before its own signature verifies; a migration refused for duplicate
  rows leaves the migrations before it applied; a Postgres stack's `DATABASE_URL` is edited in
  the compose file, not `.env`; and the legacy-email repair keeps the address for a bare stored
  value whatever its age.
