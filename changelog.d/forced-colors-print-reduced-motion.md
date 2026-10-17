### Fixed

- **Day states stay readable under a Windows contrast theme.** Forced colours repaint fills,
  shadows and border colours in the system palette, which erased the period toggle's thumb, the
  logged-day dot, and the difference between recorded-period, fertile and plain calendar days (the
  legend swatches went blank the same way). Each state is now restated in border geometry the theme
  keeps — thick solid for a recorded period, dashed for a predicted one, dotted for the start
  window, double for the fertile window, an inner ring where a prediction falls in the window — and
  the legend draws the same shapes. The toggle draws itself in system colours, today and the
  selected day get outline rings, and the dashboard cycle ribbon and the Insights cycle stack keep
  their phase colours.
- **Printing drops the interface chrome.** The header, the fixed mobile tab bar, toasts and the
  install offer no longer print, and a dark-theme page prints dark ink on a white page.
- **The dashboard's quick jumps honour reduced motion.** Jumping to the mood or symptom section
  scrolled smoothly regardless of the system's reduced-motion preference; it now jumps instantly
  when reduced motion is requested, as the link-target scroll already did.
