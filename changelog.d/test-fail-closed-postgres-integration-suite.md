none

CI and tests only: the Postgres integration suite can now fail. One helper ran
the image pull, the container start and the port lookup, and skipped on any
error, so a broken runner reported the same "docker is unavailable" verdict as a
developer machine with no docker — and the whole suite passed having tested
nothing. Only the docker-absent probe may skip now; every docker error after it
fails. `OVUMCY_REQUIRE_POSTGRES`, set on the one CI job that runs the suite,
turns that last skip into a failure too. Unset, a machine without docker skips
exactly as before. No product code.
