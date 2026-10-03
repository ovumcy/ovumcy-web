none

CI and build-recipe only: the release-tag gate also reads the Security, CodeQL and Gitleaks results, a release tag refuses to publish without the Docker Hub mirror credential, the runtime image's two Alpine packages are version-pinned, and workflow comments state which lanes run only on push. The published image's contents are unchanged.
