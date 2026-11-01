### Security

- **Linking a sign-in identity from Settings now asks you to confirm it before anything is
  linked.** After the identity provider re-authentication, the app shows a confirmation page
  naming the provider and the returned identity (its email when the provider marks it verified,
  otherwise its account identifier), and links it only when you press the confirm button. Before, the
  provider callback linked whatever identity it received on arrival; because that callback has
  to accept posts from the provider's site, someone who obtained the authorize link of your
  step-up could sign in as their own provider account and post the result into your browser,
  attaching their sign-in to your account. The confirm step is a same-origin, CSRF-protected
  form bound to your account, so that chain stops at the page. No email match is required: an
  account whose provider email differs from its own still links the same way.
