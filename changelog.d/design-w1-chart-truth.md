### Changed

- **Cycle lengths are drawn as a dot plot instead of bars.** Bar length encodes magnitude, which
  obliges the axis to start at zero — and on a zero-based axis a 28-day cycle and a 29-day one are
  the same full-height bar. The values now sit as connected points on a day axis around the data, so
  a one-day difference is visible; the average stays as a reference line, with its label inset inside
  the plot area instead of pressed against its right edge.

### Fixed

- **The temperature chart no longer draws unlogged days as 0 °C readings.** A day with no
  measurement travels as JSON `null`, and the chart converted it to the number 0, which passed every
  finite check: the first days of a cycle appeared as real points at 0 °C, the line ran through them,
  and the axis stretched from 0 °C to the highest reading — flattening the biphasic shift the chart
  exists to show. An unlogged day is now a gap: no marker, and the line breaks around it.
- **The temperature axis is scaled to the readings.** It spans the reading range plus 0.15 °C on each
  side, widened to a floor window of 0.8 °C so that a cycle with little movement is not magnified
  into noise, and it carries a tick every 0.2 °C.
- **The chart series line meets the 3:1 contrast floor for non-text content.** The light-theme line
  colour measured 2.97:1 against the card behind it; both themes' line colours were revalidated
  against the surface they are drawn on, and markers gained a ring in the surface colour so a point
  stays readable where the line passes under it.
