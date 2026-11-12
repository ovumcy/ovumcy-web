### Fixed

- **A save refused by the restore fence now says why in the server log.** When the file on the
  `/app/fence` volume cannot be written, settings saves that depend on it (webhook, calendar-feed
  link, sign-out, password and second-factor changes) fail on purpose. Until now the log showed only
  a generic save failure. It now adds a `calendar feed restore fence refused a write` line with the
  file error, such as a permission denial on a host directory mounted without the SELinux label.
