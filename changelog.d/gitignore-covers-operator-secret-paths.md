### Security

- **The ignore files now cover the sensitive paths the deployment documentation tells the operator
  to create.** `docker-compose.yml` has the operator place a file-backed app secret key under
  `./secrets/` and a private OIDC CA bundle under `./certs/`, and the backup contract writes
  database dumps and volume archives into `./backups/` while requiring they be treated as sensitive
  health data — none of those paths, nor the usual private-key and keystore extensions, were
  ignored, so following the documented procedure left a secret or a dump one inattentive `git add`
  away from a commit, and inside the context of a local `docker build`. Both `.gitignore` and
  `.dockerignore` now carry the class. A bare `*.sql` rule is deliberately absent: the tracked
  migration directory is SQL and is copied into the image, and an extension rule would swallow a
  new migration without saying so. No file of the class is tracked, and no tracked file became
  ignored; whether one was ever added and later removed is a question only a full-history scan
  answers, and that scan is a release step of its own.
