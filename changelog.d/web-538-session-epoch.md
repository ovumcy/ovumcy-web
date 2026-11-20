### Security

- **Upgrading signs everyone out once.** A restored backup could bring back a session that had been signed
  out after the backup was taken, and could let a session from before the restore resolve as a different
  account that was created afterwards and happened to reuse its id. Every session and every pending
  sign-in, two-factor or link step now carries a random session epoch the server keeps in its database and
  replaces on any boot that detects a restore. Sessions issued by earlier releases carry no epoch, so the
  first start of this release ends them all: sign in once on each device. Nothing else is lost.
