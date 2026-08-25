none

Internal only: the DOM contract between the templates and the code that
addresses them now runs both ways, and the dashboard's estimate qualifiers are
pinned.

The dashboard marks an ovulation estimate the luteal-phase clamp made inexact in
three places — the status line's approximate marker, the cycle hero's estimate
qualifier and the reminder banner's approximate marker — and nothing outside
`internal/templates` named any of them: neither the two `data-*` hooks nor the
i18n keys behind them appeared in a Go, TypeScript or CSS source, so a template
edit could drop a medical-safety qualifier with every suite green. One
regression now drives a fixture into that state and asserts the hooks per
subtree; the status line's marker had no hook at all and gains one, attributes
only, with copy and classes untouched.

The wider class behind it has a barrier now: every `data-*` attribute rendered
from `internal/templates` must be named at least once outside it — by a Go
regression, a browser script, a Playwright spec or a component rule in the
stylesheet. A hook nothing names is markup that reads like a contract and
promises coverage that does not exist. The check counts both spellings a
consumer can use, the literal attribute and the camel-cased `dataset` name, and
tokenizes rather than substring-matches: three hooks looked consumed while the
only names in the tree were their longer siblings, and a fourth — the cycle-stack
period flag — looked consumed because its one-word `dataset` name is an ordinary
English word some unrelated script happens to quote, which is why the bare-key
spelling is accepted only where a hook's name actually looks like one.
Thirty-one hooks render with no
consumer today; each is recorded with the reason it is still there, and the list
can only shrink — an entry whose hook has since gained a reader, or left the
templates, fails as loudly as a new dead hook. Two of the thirty-one are not
decoration and are named as such: the cycle-start confirm island renders an
implantation flag no dialog reads while all its siblings are read, and the
paused-estimate line carries a state key that is never asserted beside a hook
that is.

No rendered copy, status code, route or stored value changes.
