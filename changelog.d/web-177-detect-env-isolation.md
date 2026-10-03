none

CI only: the release gate runs ci.yml's detect step under `env -i` with just its own inputs, reads
the last value the step wrote for a lane, and documents why the heavy lanes accept release and
dispatch runs. A tag the gate cannot judge is still refused; no runtime behavior change.
