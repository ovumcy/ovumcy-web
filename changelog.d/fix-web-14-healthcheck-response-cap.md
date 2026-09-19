### Security

- **The container healthcheck/readycheck probe no longer trusts the port it checks to answer with a bounded response header block.** The probe's `http.Client` left `MaxResponseHeaderBytes` at Go's 10 MiB default, so anything able to bind the loopback port a healthcheck polls could make the probe buffer far more header data than a status-code check needs. The response header block is now capped at 16 KiB, matching the sizing already used for outbound webhook delivery responses.
