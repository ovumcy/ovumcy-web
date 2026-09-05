### Security

- **`.gitignore` now covers the secret paths the deployment doc tells the operator to create.**
  `docker-compose.yml` documents mounting a file-backed app secret key from `./secrets/` and a
  private OIDC CA bundle from `./certs/`, but neither directory nor the common private-key/keystore
  extensions (`*.pem`, `*.key`, `*.p12`, `*.pfx`, `*.jks`) were ignored — a secret created by
  following the compose instructions could be committed by an inattentive `git add`. No such file
  was ever tracked; this closes the gap before one is.
