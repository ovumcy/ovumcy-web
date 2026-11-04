none

Lint configuration and whitespace only, with no effect on the shipped application. Nothing in CI
refused an unformatted Go file, and six had reached main. `.golangci.yml` now enables the `gofmt`
formatter, so the existing golangci-lint step fails on one, and those six files are reformatted.
