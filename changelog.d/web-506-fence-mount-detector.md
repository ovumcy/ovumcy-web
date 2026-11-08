### Security

- **The image refuses to start with the restore fence on the container's writable layer.** A
  `docker run` with a data volume and no volume at `/app/fence` used to arm the calendar-feed
  restore fence inside the container, so every recreation of the container lost it and the next
  start revoked every owner's sessions, local password and webhook endpoints as if a backup had
  been restored. The image now sets `CALENDAR_FEED_FENCE_REQUIRE_MOUNT=true`: the fence directory
  must be a mounted volume (named volume, bind mount or Kubernetes volume, checked by device and
  `/proc/self/mountinfo`), or the start is refused, naming `CALENDAR_FEED_FENCE_PATH` and writing
  nothing. A `tmpfs` or `ramfs` there (`--tmpfs /app/fence`) is refused as well, since it loses the
  fence on every restart. The check cannot tell a volume that does not outlive the container
  (an anonymous volume, a Kubernetes `emptyDir`) from a persistent one: use a named volume, a bind
  mount or a persistent volume claim. Mount a volume at `/app/fence`, or set `CALENDAR_FEED_FENCE_OPTOUT=true` to accept
  running without a fence. A bare binary leaves the variable unset and is unchanged.
