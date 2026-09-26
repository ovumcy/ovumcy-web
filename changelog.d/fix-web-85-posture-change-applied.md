### Changed

- **JSON API:** when a password change (`PUT /api/v1/users/current/password`) or a two-factor
  enable/disable (`PUT`/`DELETE /api/v1/users/current/2fa`) has committed but its session could
  not be re-issued, the response is now `401` with category `unauthorized` instead of `500`. The
  error key is `password changed sign in again`, `two factor enabled sign in again`, or
  `two factor disabled sign in again`, replacing `failed to create session`; each means the change
  took effect and the client must sign in again (with the new password, for the password case). A
  refusal that changed nothing keeps its previous status and key.
