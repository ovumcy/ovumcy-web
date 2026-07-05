// Command mutationmerge combines the per-shard gremlins JSON reports produced
// by internal/api's sharded mutation-testing matrix (see
// .github/workflows/mutation.yml and scripts/mutation.sh, issue #161) into a
// single internal_api.json matching the naming convention every other
// baseline target (internal_services.json, internal_security.json) already
// uses, so downstream consumers (the test-suite auditor, a future
// .mutation/internal-api.md refresh) read one file instead of learning about
// N shard-specific filenames.
//
// gremlins' --output JSON shape is {"go_module": "...", "files": [{"file_name":
// "...", "mutations": [...]}, ...]}. A shard's report only contains entries
// for the files gremlins actually mutated (files scoped out via
// --exclude-files simply never appear), so merging is a concatenation of each
// shard's "files" array, sorted by file_name for a deterministic diff-stable
// output. Because internal/api's shard partition is a strict, non-overlapping
// split (scripts/mutation.sh verify-shards proves this independently), no
// file_name should ever appear in more than one shard's report — if one does,
// that means the partition and the merge have gone out of sync, so this
// fails loudly rather than silently keeping one copy.
//
// Usage: mutationmerge -in <dir> -glob <pattern> -out <file>
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type fileReport struct {
	FileName  string          `json:"file_name"`
	Mutations json.RawMessage `json:"mutations"`
}

type gremlinsReport struct {
	GoModule string       `json:"go_module"`
	Files    []fileReport `json:"files"`
}

func main() {
	inDir := flag.String("in", "", "directory to search for shard JSON reports")
	pattern := flag.String("glob", "*.json", "glob (relative to -in) matching shard report files")
	outPath := flag.String("out", "", "path to write the merged JSON report")
	flag.Parse()

	if *inDir == "" || *outPath == "" {
		fatalf("usage: mutationmerge -in <dir> -glob <pattern> -out <file>")
	}

	matches, err := findShardFiles(*inDir, *pattern)
	if err != nil {
		fatalf("find shard files: %v", err)
	}
	if len(matches) == 0 {
		fatalf("no shard report files matched %s under %s — nothing to merge", *pattern, *inDir)
	}

	merged, err := mergeReports(matches)
	if err != nil {
		fatalf("%v", err)
	}

	out, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		fatalf("marshal merged report: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		fatalf("create output dir: %v", err)
	}
	if err := os.WriteFile(*outPath, out, 0o644); err != nil {
		fatalf("write merged report: %v", err)
	}

	fmt.Printf("merged %d shard report(s), %d file(s), into %s\n", len(matches), len(merged.Files), *outPath)
}

// findShardFiles walks dir (shard reports may land in per-artifact
// subdirectories after actions/download-artifact) and returns every regular
// file whose basename matches pattern, sorted for deterministic processing
// order.
func findShardFiles(dir, pattern string) ([]string, error) {
	var matches []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ok, err := filepath.Match(pattern, d.Name())
		if err != nil {
			return err
		}
		if ok {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// mergeReports concatenates the "files" arrays of every shard report. It
// fails if a go_module mismatch is found (shards from different repos/module
// paths should never be merged together) or if the same file_name appears in
// more than one shard (the shard partition is supposed to guarantee this
// never happens; a duplicate means the partition and the shards that
// produced these reports are no longer in sync).
func mergeReports(paths []string) (*gremlinsReport, error) {
	merged := &gremlinsReport{}
	seen := make(map[string]string) // file_name -> which shard path first reported it

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var report gremlinsReport
		if err := json.Unmarshal(data, &report); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}

		if merged.GoModule == "" {
			merged.GoModule = report.GoModule
		} else if report.GoModule != "" && report.GoModule != merged.GoModule {
			return nil, fmt.Errorf("go_module mismatch: %s has %q, expected %q (from an earlier shard)", path, report.GoModule, merged.GoModule)
		}

		for _, f := range report.Files {
			if priorPath, dup := seen[f.FileName]; dup {
				return nil, fmt.Errorf("file %q reported by both %s and %s — shard partition overlap, refusing to merge", f.FileName, priorPath, path)
			}
			seen[f.FileName] = path
			merged.Files = append(merged.Files, f)
		}
	}

	sort.Slice(merged.Files, func(i, j int) bool {
		return merged.Files[i].FileName < merged.Files[j].FileName
	})

	return merged, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
