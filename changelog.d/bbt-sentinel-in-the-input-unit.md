### Fixed

- **An impossible temperature is no longer saved as an empty one.** For an account tracking in
  Fahrenheit, any entry from just above 0 °F up to 32 °F was quietly filed as "not measured" —
  the day saved without a word, the reading gone — because the "no measurement" test ran on the
  converted Celsius value instead of on what was typed. It now runs in the account's own unit: 0
  or less still means "not measured", and anything above it is judged against the physiological
  range and refused if it falls outside. The day form rejects such a value in the browser, so it
  was reachable through the JSON API, or from the form with scripts off.
- **A hidden field can no longer refuse the day it is not part of.** A save from an account that
  hides temperature, sex activity, cervical mucus, cycle factors or notes was never about those
  fields — each keeps the value already stored — yet an unexpected value arriving in one of them
  still rejected the whole entry, and the Fahrenheit account above lost the day over a
  temperature it does not even record. Such a value is now dropped before the entry is checked.
