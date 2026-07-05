package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeShardFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestFindShardFilesWalksSubdirectoriesAndFiltersByGlob(t *testing.T) {
	root := t.TempDir()
	// actions/download-artifact lands each artifact in its own subdirectory
	// when multiple artifacts match a pattern download.
	writeShardFile(t, root, filepath.Join("mutation-baseline-results-internal_api_1", "internal_api_1.json"), `{}`)
	writeShardFile(t, root, filepath.Join("mutation-baseline-results-internal_api_2", "internal_api_2.json"), `{}`)
	writeShardFile(t, root, filepath.Join("mutation-baseline-results-internal_api_1", "README.md"), `not json`)

	matches, err := findShardFiles(root, "internal_api_*.json")
	if err != nil {
		t.Fatalf("findShardFiles: %v", err)
	}
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(matches), matches)
	}
}

func TestFindShardFilesNoMatchesReturnsEmptyNotError(t *testing.T) {
	root := t.TempDir()
	matches, err := findShardFiles(root, "*.json")
	if err != nil {
		t.Fatalf("findShardFiles on empty dir: %v", err)
	}
	if len(matches) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(matches))
	}
}

func TestMergeReportsConcatenatesFilesAndSortsDeterministically(t *testing.T) {
	dir := t.TempDir()
	shard1 := writeShardFile(t, dir, "shard1.json", `{
		"go_module": "github.com/ovumcy/ovumcy-web",
		"files": [
			{"file_name": "zzz_last.go", "mutations": [{"type": "CONDITIONALS_NEGATION", "status": "KILLED", "line": 1, "column": 2}]}
		]
	}`)
	shard2 := writeShardFile(t, dir, "shard2.json", `{
		"go_module": "github.com/ovumcy/ovumcy-web",
		"files": [
			{"file_name": "aaa_first.go", "mutations": [{"type": "ARITHMETIC_BASE", "status": "LIVED", "line": 5, "column": 6}]}
		]
	}`)

	merged, err := mergeReports([]string{shard1, shard2})
	if err != nil {
		t.Fatalf("mergeReports: %v", err)
	}

	if merged.GoModule != "github.com/ovumcy/ovumcy-web" {
		t.Fatalf("unexpected go_module: %q", merged.GoModule)
	}
	if len(merged.Files) != 2 {
		t.Fatalf("expected 2 merged files, got %d", len(merged.Files))
	}
	// Sorted by file_name regardless of shard read order.
	if merged.Files[0].FileName != "aaa_first.go" || merged.Files[1].FileName != "zzz_last.go" {
		t.Fatalf("expected sorted [aaa_first.go, zzz_last.go], got [%s, %s]", merged.Files[0].FileName, merged.Files[1].FileName)
	}
}

func TestMergeReportsRejectsDuplicateFileNameAcrossShards(t *testing.T) {
	dir := t.TempDir()
	shard1 := writeShardFile(t, dir, "shard1.json", `{
		"go_module": "github.com/ovumcy/ovumcy-web",
		"files": [{"file_name": "handlers.go", "mutations": []}]
	}`)
	shard2 := writeShardFile(t, dir, "shard2.json", `{
		"go_module": "github.com/ovumcy/ovumcy-web",
		"files": [{"file_name": "handlers.go", "mutations": []}]
	}`)

	_, err := mergeReports([]string{shard1, shard2})
	if err == nil {
		t.Fatalf("expected an error for duplicate file_name across shards, got nil")
	}
}

func TestMergeReportsRejectsGoModuleMismatch(t *testing.T) {
	dir := t.TempDir()
	shard1 := writeShardFile(t, dir, "shard1.json", `{"go_module": "github.com/ovumcy/ovumcy-web", "files": []}`)
	shard2 := writeShardFile(t, dir, "shard2.json", `{"go_module": "github.com/someone/else", "files": []}`)

	_, err := mergeReports([]string{shard1, shard2})
	if err == nil {
		t.Fatalf("expected an error for go_module mismatch, got nil")
	}
}

func TestMergeReportsHandlesEmptyFilesArray(t *testing.T) {
	dir := t.TempDir()
	// A shard whose files were all "not covered" or the shard genuinely has
	// zero mutable statements can still produce a well-formed, empty report.
	shard := writeShardFile(t, dir, "shard.json", `{"go_module": "github.com/ovumcy/ovumcy-web", "files": []}`)

	merged, err := mergeReports([]string{shard})
	if err != nil {
		t.Fatalf("mergeReports: %v", err)
	}
	if len(merged.Files) != 0 {
		t.Fatalf("expected 0 files, got %d", len(merged.Files))
	}
}

func TestMergeReportsPreservesMutationsRawJSON(t *testing.T) {
	dir := t.TempDir()
	shard := writeShardFile(t, dir, "shard.json", `{
		"go_module": "github.com/ovumcy/ovumcy-web",
		"files": [
			{"file_name": "csrf.go", "mutations": [
				{"type": "CONDITIONALS_NEGATION", "status": "KILLED", "line": 27, "column": 67},
				{"type": "CONDITIONALS_NEGATION", "status": "LIVED", "line": 30, "column": 63}
			]}
		]
	}`)

	merged, err := mergeReports([]string{shard})
	if err != nil {
		t.Fatalf("mergeReports: %v", err)
	}

	var mutations []map[string]any
	if err := json.Unmarshal(merged.Files[0].Mutations, &mutations); err != nil {
		t.Fatalf("unmarshal preserved mutations: %v", err)
	}
	if len(mutations) != 2 {
		t.Fatalf("expected 2 mutations preserved, got %d", len(mutations))
	}
	if mutations[0]["status"] != "KILLED" || mutations[1]["status"] != "LIVED" {
		t.Fatalf("mutation statuses not preserved correctly: %+v", mutations)
	}
}

func TestMergeReportsRejectsUnparseableJSON(t *testing.T) {
	dir := t.TempDir()
	shard := writeShardFile(t, dir, "shard.json", `not valid json`)

	_, err := mergeReports([]string{shard})
	if err == nil {
		t.Fatalf("expected an error for unparseable shard JSON, got nil")
	}
}
