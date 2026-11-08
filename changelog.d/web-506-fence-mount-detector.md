### Security

- **The image refuses to start with the restore fence on the container's writable layer.** A
  `docker run` with a data volume and no volume at `/app/fence` used to arm the calendar-feed
  restore fence inside the container, so every recreation of the container lost it and the next
  start revoked every owner's sessions, local password and webhook endpoints as if a backup had
  been restored. The image now sets `CALENDAR_FEED_FENCE_REQUIRE_MOUNT=true`: the fence directory
  must be a mounted volume (named volume, bind mount or Kubernetes volume, checked by device and
  `/proc/self/mountinfo`), or the start is refused, naming `CALENDAR_FEED_FENCE_PATH` and writing
  nothing. Mount a volume at `/app/fence`, or set `CALENDAR_FEED_FENCE_OPTOUT=true` to accept
  running without a fence. A bare binary leaves the variable unset and is unchanged.
