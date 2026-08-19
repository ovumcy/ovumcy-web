none

Test-only cleanup: the wait that proves a settings-interface save reached the
server existed twice — once in the language helper, once inline in the theme
scenario that had just gained it. Both were the same request-bound wait on the
same endpoint, and two copies drift: a fix applied to either would have missed
the other. They now call one `saveInterfaceSettingsForm`, with the
success-flash readback opt-in, because the callers that only need the save not
to be raced leave `/settings` before that notice is rendered.

`saveSettingsLanguage` keeps its signature and its behaviour, so the four specs
importing it are unchanged.
