### Security

- **A page left open for one account can no longer restore a file into, or change the cycle
  settings of, another account signed in later in the same browser.** The session cookie is
  browser-wide and the CSRF token is not bound to an account, so the settings restore (`POST
  /api/v1/imports/json`) and the cycle settings save (`PATCH /api/v1/users/current/cycle`, also the
  dashboard's usage-goal switch) wrote into whichever account was signed in at that moment. Both
  now carry the binding of the account that rendered the page, as the day forms already do, and
  a request whose binding names another account, or a request from a page that sends none, is
  refused with 409 and writes nothing. A JSON API client that sends no binding keeps its
  behaviour.
