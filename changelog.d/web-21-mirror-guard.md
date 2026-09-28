### Internal

- **The Docker Hub mirror steps are held free of the runner keys that let a failure through.** The
  mirror's login, its signed copy and its anonymous read-back may carry no `continue-on-error:`,
  and no `if:` other than the one that turns the mirror on; a mutant adding the former to the copy
  step fails the check.
