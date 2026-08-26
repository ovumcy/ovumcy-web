none

Dead-code removal with no visual effect, plus the check that finds this class.
One component utility nothing renders (`status-transient`, which the build never
emitted at all) and three custom properties nothing reads (`--bg-soft` in both
themes, and the two `-solid` calendar readings orphaned when the day-tag
utilities went) leave `web/src/css/input.css`. The built stylesheet drops 101
bytes and is otherwise byte-identical, so nothing paints differently.

The new report walks the stylesheet's own `@source` list and names any
`@utility` no source spells and any custom property nothing reads. It treats a
property fetched by name at runtime as read — the chart palette pulls
`--chart-grid` and `--chart-line` through `getPropertyValue`, which no `var()`
scan can see — so those stay out of the report and in the stylesheet.
