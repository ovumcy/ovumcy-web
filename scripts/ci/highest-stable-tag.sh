#!/usr/bin/env bash
# Is the given tag the highest stable release tag of this repository?
#
#   highest-stable-tag.sh vMAJOR.MINOR.PATCH
#
# The one place that answers it. docker-image.yml asks it before a build and
# again right before each write of `latest`, and promote-latest.yml asks it
# before it verifies anything and again right before its first write; each
# caller keeps its own decision (drop `latest`, refuse, stop) and its own
# words, and reads the verdict from the exit status:
#
#   0   the tag is the highest stable release tag
#   1   a higher stable release tag exists
#   2   the tag listing could not be read (the answer is unknown)
#   3   the argument is not a stable vMAJOR.MINOR.PATCH tag at all
#   64  the script was called wrongly (no argument, no GITHUB_REPOSITORY)
#   70  anything else that went wrong inside it
#
# Nothing but those statuses means "highest": an unforeseen failure ends in 70,
# never in the 1 that a caller reads as "a higher tag exists".
#
# On 0 and 1 the highest stable tag is printed on stdout, for the caller's
# message. The listing is `git/matching-refs/tags/v`, asked up to three times;
# versions compare as numbers (`sort -V`: v1.10.0 is above v1.9.2). The asked
# tag is added to the list it is compared with, so a listing that lags the very
# push being judged cannot make the tag lose to the one below it. A pre-release
# or a build suffix never counts, on either side of the comparison.
set -euo pipefail
trap 'exit 70' ERR

if [ "$#" -ne 1 ] || [ -z "${GITHUB_REPOSITORY:-}" ]; then
  echo "usage: GITHUB_REPOSITORY=owner/name highest-stable-tag.sh vMAJOR.MINOR.PATCH" >&2
  exit 64
fi
tag="$1"

stable='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
if [[ ! "$tag" =~ $stable ]]; then
  exit 3
fi

refs=""
for attempt in 1 2 3; do
  if refs="$(gh api --paginate "repos/${GITHUB_REPOSITORY}/git/matching-refs/tags/v" --jq '.[].ref')"; then
    break
  fi
  refs=""
  sleep $(( attempt * 2 ))
done
if [ -z "$refs" ]; then
  exit 2
fi

stable_tags="$(printf '%s\n' "$refs" | sed 's#^refs/tags/##' | { grep -E "$stable" || true; })"
highest="$(printf '%s\n%s\n' "$stable_tags" "$tag" | { grep -E "$stable" || true; } | sort -V | tail -n 1)"
printf '%s\n' "$highest"

if [ "$highest" = "$tag" ]; then
  exit 0
fi
exit 1
