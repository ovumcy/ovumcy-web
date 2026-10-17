### Security

- **A dashboard left open after a different account signs in no longer receives that account's
  status header, and says so.** After a save the Today page fetches the dashboard again to refresh
  its status header. That request now names the account the page was rendered for, and the server
  refuses it with the same "this page belongs to another account" answer the calendar gives, sending
  none of the other account's page. The tab shows the account-changed notice under the journal
  instead of dropping the refresh without a word. Opening the dashboard by its address, reloading
  it, following a link and navigating inside the app are unchanged.
