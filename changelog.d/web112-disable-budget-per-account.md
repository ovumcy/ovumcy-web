### Security

- **Mistyped passwords while turning off two-factor sign-in no longer lock out other accounts on
  the same network.** The password check that confirms turning off two-factor sign-in counted
  failed attempts per network address, so on an instance shared behind one address (a household
  router) one owner's mistakes could block another owner from turning it off. The count is now
  kept per account and address, the same way the other password-confirmed Settings actions keep
  theirs. The attempt limit per account is unchanged.
