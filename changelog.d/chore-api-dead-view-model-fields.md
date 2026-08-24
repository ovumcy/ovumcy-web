none

Internal only: dead declarations removed from `internal/api`, with a barrier to
keep them out. `api.CalendarDay` carried twenty-three fields and the calendar
template reads twelve; the other eleven were the raw state predicates
`buildCalendarDays` had already resolved into the cell's classes and its stable
state key, and one of them, `BadgeClass`, was the third output of every branch
of a nine-branch ladder that nothing has ever rendered. `export_types.go`, a
production file whose single declaration was a type alias only one test file
named, is gone; the test names the service type that owns the shape. No
rendered markup, status code or error key changes.

The new barrier holds every exported field of a view-model struct, and every
type declared in `internal/api`, to a reader in an embedded template or in
production Go — a class `deadcode` cannot report, since it analyses functions
only. The six `calendar-tag-*` utilities the removed field composed stay in
`web/src/css/input.css` for now: removing a utility is not finished until a
rebuild shows it left the embedded bundle.

The export range now resolves through the one lookup that already covered both
a query string and a form body, with the framework resolution order it depends
on pinned rather than compensated for; two test cases that asserted a class no
branch can emit and a title comparison the code no longer makes were rewritten
to fail on the behaviour they are named for.
