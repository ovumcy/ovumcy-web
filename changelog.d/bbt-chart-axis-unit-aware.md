### Fixed

- The stats page's BBT chart now scales its axis floor window and tick step to the owner's
  temperature unit. A °C axis kept its 0.8 °C floor and 0.2 °C tick; a °F axis (converted
  server-side, per #789) used to inherit those same numbers unscaled, cramming a °F reading's
  ~0.3 °C biphasic shift into a fifth of the visual range a °C owner sees. It now uses a 1.44 °F
  floor (0.8 °C × 1.8) and a 0.4 °F tick — the nearest readable step to the unscaled 0.36 °F.
