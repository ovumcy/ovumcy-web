### Fixed

- **The app now refuses to start over storage it can read but not write.** A database or `/app/fence`
  directory that the app user cannot write to used to start without complaint. After that, every webhook,
  calendar-feed, sign-out and password save failed with a generic error. A database that took no writes
  could also leave the fence out of step, and the next start then signed everyone out. Startup now makes
  a test write to both and stops with the path named. The self-hosting guide has a new section on volume
  permissions under Podman and SELinux.
