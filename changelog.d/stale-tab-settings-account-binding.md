### Security

- **A settings or onboarding page left open can no longer write to the account that signed in
  after it.** The browser keeps one session, and a page rendered for one account keeps working after
  another account signs in elsewhere in the same browser, so a stale tab could save its form onto
  the second account. The cycle settings and the restore were bound to their rendering account
  earlier; the rest of the class now is too: the profile, interface, tracking, reminder, timezone,
  webhook and calendar-feed writes, the custom-symptom writes, and every onboarding step. A write
  from a page that names another account, or from a page that names none, is refused with 409 and
  changes nothing ("You're now signed in to a different account. This change was not saved. Reload
  the page."). The browser's own timezone sync sends the binding too. API clients that render no page
  and send no binding keep their behaviour; a binding they do send is still checked.
- **The language switcher no longer stores its choice on the wrong account.** The switcher is shown
  on the onboarding pages and on signed-out pages, and both outlive a sign-in elsewhere in the same
  browser, so a stale tab could save its language onto whichever account was signed in by then. The
  choice is now stored on the account only when the form carries the binding of the account now
  signed in; any other switch changes the language cookie alone, and the answer is the same either
  way.
