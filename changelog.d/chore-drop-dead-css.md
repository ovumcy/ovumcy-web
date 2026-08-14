none

Dead-code removal with no visual effect: nineteen component utilities nothing
renders, ten custom properties nothing reads, and the four rules inside live
components that still pointed at removed classes. The `settings-tracking-toggle`
class goes from the settings labels with its now-empty utility — after the
removal it carried no declaration and painted nothing. Bundle drops ~1 KB.
