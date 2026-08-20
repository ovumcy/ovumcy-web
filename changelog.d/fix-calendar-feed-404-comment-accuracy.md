none

Comment-only correction in the calendar feed handler: the security envelope and
the two inline notes claimed the uniform bad-token 404 carries "no body". It
carries Fiber's fixed status text under `text/plain` — `c.SendStatus` fills an
empty body — so the promise as written was false in its detail while true in its
substance, and a reader checking the route against it would have looked for
something the handler never does. The wording now says what `SendStatus` sends
and why it is still no oracle: the same bytes whatever the cause was.
