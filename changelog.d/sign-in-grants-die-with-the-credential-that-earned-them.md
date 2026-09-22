### Security

- **Upgrading signs everyone out.** Session tokens and password-reset tokens are now signed under
  separate keys derived from `SECRET_KEY`, and each carries its own audience and type, so a token
  minted for one purpose can never be accepted as the other. Every token minted before the upgrade
  stops verifying: **all sessions end, open password-reset links stop working, and a sign-in
  waiting at the two-factor step has to start again.** Nothing needs re-configuring; owners sign
  in again. Rolling back to the previous release makes the old tokens valid again.

- **A reset link or a pending two-factor step no longer outlives the credential behind it.** A
  password-reset link, and the step between the password and the two-factor code, now die when
  the account's password, recovery code, two-factor setting or sessions change after they were
  issued — including a change that lands while the reset is being submitted. An account an
  operator has flagged for a forced password change can no longer finish a sign-in through a
  two-factor step started before the flag.

- **Two submits of the same reset link now get a clear answer.** The one that loses gets the
  same "invalid reset link" answer as a replay, and its reset cookie is cleared.

- **Messages carried across a redirect expire on the server.** A kept message cookie is ignored
  once its few minutes are up, and signing out clears it.

- **A successful sign-in forgives only its own client.** A sign-in, recovery-code sign-in or
  two-factor step that succeeds clears the failure count of the browser that succeeded, and no
  longer the count other clients built up against the same account; that count ages out on its
  own. Checks that already need a signed-in session — the password re-check in settings, the
  password change and turning two-factor off — still clear the account's count on success, so an
  owner's own typos never add up to a lockout. A sign-in with an address that normalizes to
  nothing is refused before any lookup.
