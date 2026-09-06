### Fixed

- **An impossible temperature is no longer saved as an empty one.** For an account tracking in
  Fahrenheit, any entry from just above 0 °F up to 32 °F was quietly filed as "not measured" —
  the day saved without a word, the reading gone — because the "no measurement" test ran on the
  converted Celsius value instead of on what was typed. It now runs in the account's own unit: 0
  or less still means "not measured", and anything above it is judged against the physiological
  range and refused if it falls outside. The day form rejects such a value in the browser, so it
  was reachable through the JSON API, or from the form with scripts off.
- **A hidden field can no longer refuse the day it is not part of.** A form save from an account
  that hides temperature or cycle factors no longer loses the whole entry to a value in the hidden
  field: the form's hidden fields are not read at all — on an existing day the stored reading is
  kept, on a new day the field starts empty. JSON bodies carry every field they send.
