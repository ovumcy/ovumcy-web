### Changed

The image publish workflow now reads the SBOM attestation back off the pushed
digest before any public tag is written: every platform the release ships must
carry an SPDX document with packages, or the run fails with the image still
untagged. Until now CI verified the signature and the build provenance but
never the SBOM that README.md tells operators to inspect.
