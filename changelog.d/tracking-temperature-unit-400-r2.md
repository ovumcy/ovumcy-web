### Fixed

- **A tracking-settings save with an unrecognised `temperature_unit` is refused with `400`.** `fahrenheit`, `kelvin` and the like used to answer `200` and silently store `c`; only an omitted unit still reads as `c`.
