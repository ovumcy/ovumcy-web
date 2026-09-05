none

CI/tooling-only fix: `scripts/publishorder`'s promotion guard checked a tag's
PUT body with `strings.Contains(body, digest)`, so a manifest mutated between
the GET and the PUT (JSON-equivalent, different SHA-256) still satisfied it as
long as the digest substring stayed present. The public-check fixture had the
same shape — it handed the stub a digest to answer with instead of hashing the
body actually stored under a tag, so the oracle could confirm itself. Both now
compare or compute real bytes: the promotion assertion is byte-exact against
the manifest the stub served, and the public-check stub hashes (`sha256sum`)
the body a case supplies rather than being told the answer. This is a test-only
change; no defect in `docker-image.yml` itself is claimed or was found — its
promote step already forwards the downloaded manifest file unchanged.
