### Fixed

- **The Docker Hub mirror no longer fails its own signature check by asking too early.** The release
  job signs the mirrored digest on Docker Hub and reads that signature back before it writes a
  public tag there. Docker Hub accepts the signature before it lists it: the read-back ran about a
  second after the write, found nothing, and failed the job with the image mirrored but untagged —
  so no unverifiable tag was published, and no verifiable one either. The read-back now waits for
  the registry's listing to catch up, up to a minute, and still fails the job when the signature
  never appears.
