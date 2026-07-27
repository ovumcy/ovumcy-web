package main

import (
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// buildVersion is the release identity injected at build time:
//
//	go build -ldflags "-X main.buildVersion=<version>"
//
// The Dockerfile forwards its BUILD_REVISION build-arg here. It stays empty
// for builds that do not pass the flag (go run, plain go build); the asset
// cache-bust token then falls back to VCS or process-start identity.
var buildVersion string

// buildRevision is the identity the startup banner prints. It reads the process
// build info and defers every decision to resolveBuildRevision, which is pure
// and testable on explicit inputs.
func buildRevision() string {
	info, _ := debug.ReadBuildInfo() // codecov:ignore -- process-wide read; the decision it feeds is covered on explicit inputs
	return resolveBuildRevision(buildVersion, info)
}

// resolveBuildRevision picks what the banner reports, preferring the VCS stamp
// over the ldflags one — the opposite order from assetCacheBustToken, and
// deliberately so. The asset token wants one stable value per release, so a
// release stamp outranks everything. The banner is a truth claim about the
// running binary, and only the VCS stamp can say the tree was dirty, so a build
// carrying both must not let the release stamp hide that.
//
// The ldflags fallback is what makes the field usable in the shipped image at
// all: `.dockerignore` excludes `.git`, so a container build carries no vcs.*
// setting and this used to report "unknown" for every published image — exactly
// where an operator rolling back to a previous tag needs to know which revision
// booted. The Dockerfile already forwards its BUILD_REVISION build-arg into
// main.buildVersion for the asset token; the banner now reads the same stamp.
func resolveBuildRevision(ldflagsVersion string, info *debug.BuildInfo) string {
	if revision, modified := vcsRevisionFromBuildInfo(info); revision != "" {
		if modified {
			return revision + "-dirty"
		}
		return revision
	}
	if version := strings.TrimSpace(ldflagsVersion); version != "" {
		return version
	}
	return "unknown"
}

// vcsRevisionFromBuildInfo extracts the raw vcs.revision stamped into info
// and whether the working tree was modified. revision is "" when info is nil
// or carries no usable revision — `go run` never stamps VCS settings, and
// neither does a build from a tree without .git (the Docker build context
// only copies the source directories).
func vcsRevisionFromBuildInfo(info *debug.BuildInfo) (revision string, modified bool) {
	if info == nil {
		return "", false
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if strings.TrimSpace(setting.Value) != "" {
				revision = setting.Value
			}
		case "vcs.modified":
			modified = strings.TrimSpace(setting.Value) == "true"
		}
	}
	return revision, modified
}

// assetVersionShortRevisionLength trims a full 40-char commit sha to a short
// prefix so the "-dirty" marker still fits within the api layer's 16-char
// asset-version token cap; a prefix is just as good a cache-bust token as the
// full sha.
const assetVersionShortRevisionLength = 10

// resolveAssetVersion picks the cache-busting token for versioned static
// asset URLs (?v=<token>): the ldflags-injected buildVersion when set (the
// release image path), else the short VCS revision when the binary carries
// one (go build from a git checkout), else a process-start timestamp — so a
// from-source deployment (`go run`, .git-less build) gets a token that
// changes per start instead of the shared constant "unknown" every such
// build used to serve, which let stale cached assets survive upgrades.
func resolveAssetVersion() string {
	info, _ := debug.ReadBuildInfo()
	return assetCacheBustToken(buildVersion, info, time.Now())
}

// assetCacheBustToken implements resolveAssetVersion's fallback chain on
// explicit inputs so each step stays unit-testable.
func assetCacheBustToken(ldflagsVersion string, info *debug.BuildInfo, processStart time.Time) string {
	if version := strings.TrimSpace(ldflagsVersion); version != "" {
		return version
	}
	if revision, modified := vcsRevisionFromBuildInfo(info); revision != "" {
		revision = strings.TrimSpace(revision)
		if len(revision) > assetVersionShortRevisionLength {
			revision = revision[:assetVersionShortRevisionLength]
		}
		if modified {
			return revision + "-dirty"
		}
		return revision
	}
	return "dev-" + strconv.FormatInt(processStart.Unix(), 10)
}
