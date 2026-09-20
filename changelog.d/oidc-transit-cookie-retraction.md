### Security

- **An OIDC transit cookie the server refuses is retracted in the same response.** The sign-in
  state, step-up and step-up continuation cookies used to stay in the browser after the server had
  refused to read them — an unopenable envelope, a plaintext that is not the payload, a payload
  past its own expiry — and kept being sent to the callback path until they expired on their own.
  Each reader now clears the value in the response that refused it, matching what the TOTP cookies
  already do. A value the server still honours is left in place, so a stray or cross-site request
  to the callback path cannot cancel a sign-in or a step-up in progress.
