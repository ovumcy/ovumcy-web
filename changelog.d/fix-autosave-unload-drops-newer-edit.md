### Fixed

- **Leaving the dashboard no longer drops a journal edit made while a save was still running.** The
  autosave coalesces on the request already in flight, so an edit typed during a slow save bumped the
  pending version but queued nothing; the only thing that would have carried it was the re-arm timer
  that runs after the response, which a page unload never reaches. Closing the tab or navigating away
  in that window therefore left the newer value on screen and the older one on the server, with no
  error shown. The unload flush now sends the current form state on its own keepalive request
  whenever it is newer than the save on the wire — once per version, so a cancelled navigation does
  not resend it. The ordinary debounce path is unchanged.
