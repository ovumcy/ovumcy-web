### Security

- **The README and the self-hosted guide carry interim steps for three v2.0.0 defects.** A new
  "Known Issues in 2.0.0" section in `docs/self-hosted.md`, linked from the README Quick Start,
  tells operators to run the container read-only with the `ovumcy_fence` volume (a `docker run`
  on a writable container filesystem without it revokes every local password, recovery code,
  session and webhook endpoint on each recreation, with the recovery steps for an instance already
  hit), to take backups only with the server stopped and no mutating operator command running (a
  backup taken while it ran is restored with the fence file removed), and to check the dashboard
  date before recording a day after signing out. The backup paragraphs that allowed a live snapshot
  or a live `pg_dump` point to it.
