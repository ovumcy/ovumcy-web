### Security

- **The calendar-feed opt-out residual now names restored feeds.** With `CALENDAR_FEED_FENCE_OPTOUT=true`, a backup taken before the opt-out period, restored with the old fence volume mounted again, brings back a feed that was revoked meanwhile. This is a declared residual of the opt-out: the docs now state it with the closing steps (mount the fence, remove the opt-out, remove the fence file after such a restore, re-generate feeds), and a test pins it.
