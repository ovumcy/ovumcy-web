### Security

- CI now refuses a trivy-fs gate waiver without a path scope, a statement, or an expiry within six months, and fails when the Security-tab scan and the gate scan drift apart or an ignore file reaches any other Trivy scan.
