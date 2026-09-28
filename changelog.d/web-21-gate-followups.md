### Internal

- **The release gates refuse the runner keys that would let them fail open.** A step's
  `continue-on-error:` or `if:`, or a job's `continue-on-error:`, lets the runner pass a gate whose
  script refused — which no test that runs the script can see. The tag-ancestry gate and the publish
  job's steps from the push through the anonymous read-back of the public tags are now held free
  of those keys by one shared reader. The
  ancestry fixture now mirrors the runner's checkout of a tag (detached, no local `main`), and an
  accepted tag is recognised by its own acceptance line rather than by text the refusal also
  contains.
