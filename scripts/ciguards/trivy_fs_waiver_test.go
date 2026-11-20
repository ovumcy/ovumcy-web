package ciguards

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// trivy-fs is two scans of one tree: a SARIF scan with no ignore file feeding
// the Security tab, and a gate that reads the dated waivers in
// fsGateWaiverFile. The guards below keep that waiver from drifting the two
// apart, from outliving its reason, and from reaching any other scan.

const (
	fsGateWaiverFile = ".trivyignore.fs-gate.yaml"
	trivyFSScanStep  = "Run Trivy filesystem scan"
	trivyFSGateStep  = "Gate on the Trivy filesystem scan"
	// maxWaiverHorizon bounds how far out a waiver's expired_at may sit: a
	// waiver is re-judged at least twice a year, never renewed open-ended.
	maxWaiverHorizon = 183 * 24 * time.Hour
)

// stepRunLine returns a step's single-line `run:` command. A step-level env or
// shell could change how one step runs the same line, so either is refused.
func stepRunLine(step string) (string, error) {
	var run []string
	for line := range strings.SplitSeq(step, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "run:"):
			run = append(run, strings.TrimSpace(strings.TrimPrefix(trimmed, "run:")))
		case strings.HasPrefix(trimmed, "env:"), strings.HasPrefix(trimmed, "shell:"):
			return "", fmt.Errorf("the step declares %q, which can change how this step alone runs Trivy", trimmed)
		}
	}
	if len(run) != 1 || run[0] == "" || strings.ContainsAny(run[0][:1], "|>") {
		return "", fmt.Errorf("the step has no single-line `run:` command (found %q)", run)
	}
	return run[0], nil
}

// withoutFlags removes each named flag and the value after it from args. Every
// flag must appear exactly once, and with the given value unless that is "".
func withoutFlags(args []string, flags map[string]string) ([]string, error) {
	seen := map[string]bool{}
	var rest []string
	for i := 0; i < len(args); i++ {
		want, ok := flags[args[i]]
		if !ok {
			rest = append(rest, args[i])
			continue
		}
		flag := args[i]
		if seen[flag] {
			return nil, fmt.Errorf("%s appears twice", flag)
		}
		seen[flag] = true
		if i+1 == len(args) {
			return nil, fmt.Errorf("%s has no value", flag)
		}
		i++
		if want != "" && args[i] != want {
			return nil, fmt.Errorf("%s is %q, want %q", flag, args[i], want)
		}
	}
	for flag := range flags {
		if !seen[flag] {
			return nil, fmt.Errorf("%s is missing", flag)
		}
	}
	return rest, nil
}

// TrivyFSStepsScanAlike refuses a SARIF scan and a gate that would judge
// different findings: once each step's own output flags are taken out, the
// two command lines must be identical, scanner, severity, dev deps, skip-dirs
// and image included. The gate alone reads the waiver file and fails on a
// finding; the scan alone writes SARIF and never fails on one.
func TrivyFSStepsScanAlike(scan, gate string) error {
	for name, line := range map[string]string{"SARIF scan": scan, "gate": gate} {
		if err := singleTrivyCommand(strings.Fields(line)); err != nil {
			return fmt.Errorf("the %s: %w", name, err)
		}
	}
	s, err := withoutFlags(strings.Fields(scan), map[string]string{"--format": "sarif", "--output": "", "--exit-code": "0"})
	if err != nil {
		return fmt.Errorf("the SARIF scan: %w", err)
	}
	g, err := withoutFlags(strings.Fields(gate), map[string]string{"--format": "table", "--ignorefile": fsGateWaiverFile, "--exit-code": "1"})
	if err != nil {
		return fmt.Errorf("the gate: %w", err)
	}
	if !slices.Equal(s, g) {
		return fmt.Errorf("the SARIF scan and the gate scan differently, so the Security tab and the gate can disagree:\n  scan: %s\n  gate: %s", strings.Join(s, " "), strings.Join(g, " "))
	}
	return nil
}

// singleTrivyCommand refuses a run line that is more than one `docker run`
// of the tree: shell control such as `|| true` on both lines keeps them alike
// while the gate can no longer fail.
func singleTrivyCommand(args []string) error {
	if len(args) < 2 || args[0] != "docker" || args[1] != "run" || args[len(args)-1] != "." {
		return fmt.Errorf("the run line is not one `docker run ... .` command")
	}
	for _, a := range args {
		if strings.ContainsAny(a, "|;&`<>") || strings.Contains(a, "$(") {
			return fmt.Errorf("the run line carries shell control %q", a)
		}
	}
	return nil
}

func trivyFSRunLines(t *testing.T) (scan, gate string) {
	t.Helper()
	block := workflowfile.Job(t, securityWorkflow, "trivy-fs")
	for _, s := range []struct {
		name string
		into *string
	}{{trivyFSScanStep, &scan}, {trivyFSGateStep, &gate}} {
		run, err := stepRunLine(workflowfile.Step(t, securityWorkflow, "trivy-fs", block, s.name))
		if err != nil {
			t.Fatalf("trivy-fs step %q: %v", s.name, err)
		}
		*s.into = run
	}
	return scan, gate
}

func TestTrivyFSScanAndGateScanAlike(t *testing.T) {
	scan, gate := trivyFSRunLines(t)
	if err := TrivyFSStepsScanAlike(scan, gate); err != nil {
		t.Fatal(err)
	}
}

// TestTheTrivyFSGateCannotPassOnAFinding holds the runner to the gate's
// `--exit-code 1`: a step `if:` or `continue-on-error:`, or a job-level one,
// would let a finding pass with the run line untouched.
func TestTheTrivyFSGateCannotPassOnAFinding(t *testing.T) {
	block := workflowfile.Job(t, securityWorkflow, "trivy-fs")
	if found := workflowfile.JobFailOpenKeys(block); len(found) > 0 {
		t.Errorf("the trivy-fs job carries %v, so a failed gate still concludes it successful", found)
	}
	step := workflowfile.Step(t, securityWorkflow, "trivy-fs", block, trivyFSGateStep)
	if found := workflowfile.StepFailOpenKeys(step); len(found) > 0 {
		t.Errorf("the trivy-fs gate step carries %v, so the runner can pass it whatever Trivy finds", found)
	}
}

func TestTrivyFSStepsScanAlikeRefusesDrift(t *testing.T) {
	const (
		base = `docker run --rm -w /work "img" fs --scanners vuln --severity HIGH,CRITICAL --include-dev-deps --skip-dirs .tmp`
		scan = base + " --format sarif --output out.sarif --exit-code 0 ."
		gate = base + " --ignorefile " + fsGateWaiverFile + " --format table --exit-code 1 ."
	)
	if err := TrivyFSStepsScanAlike(scan, gate); err != nil {
		t.Fatalf("the aligned fixture was refused: %v", err)
	}
	for name, pair := range map[string][2]string{
		"severity drift":          {scan, strings.Replace(gate, "HIGH,CRITICAL", "CRITICAL", 1)},
		"dev deps dropped":        {strings.Replace(scan, " --include-dev-deps", "", 1), gate},
		"extra skip-dirs":         {scan, strings.Replace(gate, " --format", " --skip-dirs web --format", 1)},
		"gate does not fail":      {scan, strings.Replace(gate, "--exit-code 1", "--exit-code 0", 1)},
		"gate exit-code inlined":  {scan, strings.Replace(gate, "--exit-code 1", "--exit-code=1", 1)},
		"scan reads the waiver":   {strings.Replace(scan, " --format", " --ignorefile "+fsGateWaiverFile+" --format", 1), gate},
		"gate reads another file": {scan, strings.Replace(gate, fsGateWaiverFile, ".trivyignore", 1)},
		"scan can fail":           {strings.Replace(scan, "--exit-code 0", "--exit-code 1", 1), gate},
		"or-true on both":         {scan + " || true", gate + " || true"},
		"true-or prefix on both":  {"true || " + scan, "true || " + gate},
	} {
		if err := TrivyFSStepsScanAlike(pair[0], pair[1]); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestStepRunLineRefusesAStepLevelEnv(t *testing.T) {
	for _, key := range []string{"env:\n          TRIVY_SEVERITY: CRITICAL", "shell: sh"} {
		if _, err := stepRunLine("        " + key + "\n        run: trivy fs .\n"); err == nil {
			t.Errorf("a step-level %q was accepted", key)
		}
	}
}

// fsGateWaiver is one entry of fsGateWaiverFile.
type fsGateWaiver struct {
	section, id, expiredAt, statement string
	paths                             []string
	line                              int
}

// waiverSections are the top-level lists Trivy's YAML ignore file reads.
var waiverSections = map[string]bool{"vulnerabilities": true, "misconfigurations": true, "secrets": true, "licenses": true}

// parseFSGateWaivers reads the YAML subset fsGateWaiverFile is written in:
// section lists of entries with block-style keys. It fails closed — any other
// shape is an error, never an entry skipped unjudged.
func parseFSGateWaivers(text string) ([]fsGateWaiver, error) {
	var (
		entries []fsGateWaiver
		section string
		cur     *fsGateWaiver
		keys    map[string]bool
		lastKey string
		inBlock bool
	)
	flush := func() {
		if cur != nil {
			entries = append(entries, *cur)
		}
	}
	setKey := func(n int, kv string) error {
		key, value, ok := strings.Cut(kv, ":")
		if !ok {
			return fmt.Errorf("line %d: %q is not a key", n, kv)
		}
		value, _, _ = strings.Cut(value, " #")
		value = strings.TrimSpace(value)
		if keys[key] {
			return fmt.Errorf("line %d: %s set twice in one entry", n, key)
		}
		keys[key], lastKey = true, key
		switch key {
		case "id":
			cur.id = value
		case "expired_at":
			cur.expiredAt = strings.Trim(value, `"'`)
		case "paths":
			if value != "" {
				return fmt.Errorf("line %d: paths must be a block list, got %q", n, value)
			}
		case "statement":
			switch value {
			case ">", ">-", ">+", "|", "|-", "|+":
				inBlock = true
			default:
				cur.statement = strings.Trim(value, `"'`)
			}
		default:
			return fmt.Errorf("line %d: key %q is not one this guard judges — teach it the key before using it", n, key)
		}
		return nil
	}
	for i, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		n := i + 1
		trimmed := strings.TrimSpace(line)
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if inBlock {
			if trimmed == "" || indent >= 6 {
				cur.statement = strings.TrimSpace(cur.statement + " " + trimmed)
				continue
			}
			inBlock = false
		}
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.Contains(line, "\t") {
			return nil, fmt.Errorf("line %d: tab character", n)
		}
		switch {
		case indent == 0:
			name, value, _ := strings.Cut(trimmed, ":")
			if !waiverSections[name] || (strings.TrimSpace(value) != "" && strings.TrimSpace(value) != "[]") {
				return nil, fmt.Errorf("line %d: %q is not a known section header", n, trimmed)
			}
			flush()
			section, cur = name, nil
		case indent == 2 && strings.HasPrefix(trimmed, "- ") && section != "":
			flush()
			cur, keys = &fsGateWaiver{section: section, line: n}, map[string]bool{}
			if err := setKey(n, strings.TrimPrefix(trimmed, "- ")); err != nil {
				return nil, err
			}
		case indent == 4 && cur != nil:
			if err := setKey(n, trimmed); err != nil {
				return nil, err
			}
		case indent == 6 && cur != nil && lastKey == "paths" && strings.HasPrefix(trimmed, "- "):
			item, _, _ := strings.Cut(strings.TrimPrefix(trimmed, "- "), " #")
			cur.paths = append(cur.paths, strings.Trim(strings.TrimSpace(item), `"'`))
		default:
			return nil, fmt.Errorf("line %d: %q is outside the shape this guard reads", n, trimmed)
		}
	}
	flush()
	return entries, nil
}

// FSGateWaiversAreBounded refuses a waiver that could outlive its reason or
// reach past the finding it was granted for: every entry names an id and at
// least one path, carries a statement, and expires no more than
// maxWaiverHorizon after today. A past date is left to the gate, which fails
// on it by itself.
func FSGateWaiversAreBounded(text string, today time.Time) error {
	entries, err := parseFSGateWaivers(text)
	if err != nil {
		return err
	}
	var problems []string
	for _, e := range entries {
		at := fmt.Sprintf("%s entry at line %d (%s)", e.section, e.line, e.id)
		if e.id == "" {
			problems = append(problems, at+": no id")
		}
		if len(e.paths) == 0 || slices.Contains(e.paths, "") {
			problems = append(problems, at+": no paths, so it waives the id wherever it appears")
		}
		for _, p := range e.paths {
			if strings.ContainsAny(p, "*?[{") {
				problems = append(problems, fmt.Sprintf("%s: path %q is a glob, which waives the id beyond the file it was granted for", at, p))
			}
		}
		if e.statement == "" {
			problems = append(problems, at+": empty statement")
		}
		expiry, err := time.Parse(time.DateOnly, e.expiredAt)
		switch {
		case e.expiredAt == "":
			problems = append(problems, at+": no expired_at, so it never fails again")
		case err != nil:
			problems = append(problems, fmt.Sprintf("%s: expired_at %q is not a YYYY-MM-DD date", at, e.expiredAt))
		case expiry.Sub(today) > maxWaiverHorizon:
			problems = append(problems, fmt.Sprintf("%s: expired_at %s is more than %d days after %s", at, e.expiredAt, int(maxWaiverHorizon.Hours()/24), today.Format(time.DateOnly)))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s:\n  %s", fsGateWaiverFile, strings.Join(problems, "\n  "))
	}
	return nil
}

func TestFSGateWaiversAreBounded(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), fsGateWaiverFile))
	if err != nil {
		t.Fatalf("read %s, which the trivy-fs gate names: %v", fsGateWaiverFile, err)
	}
	if err := FSGateWaiversAreBounded(string(raw), time.Now().UTC().Truncate(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
}

func TestFSGateWaiversAreBoundedRefusesAnUnboundedEntry(t *testing.T) {
	today := time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)
	const valid = "vulnerabilities:\n  - id: CVE-1\n    paths:\n      - package-lock.json\n    expired_at: 2027-02-18\n    statement: >-\n      no fix,\n      dev-only.\n"
	for _, text := range []string{valid, strings.Replace(valid, "2027-02-18", "2027-02-18 # renew", 1)} {
		if err := FSGateWaiversAreBounded(text, today); err != nil {
			t.Fatalf("a bounded entry was refused: %v", err)
		}
	}
	entries, err := parseFSGateWaivers(valid)
	if err != nil {
		t.Fatalf("the valid fixture does not parse: %v", err)
	}
	if len(entries) != 1 || entries[0].statement != "no fix, dev-only." {
		t.Fatalf("the folded statement was misread: %+v", entries)
	}
	for name, text := range map[string]string{
		"no expired_at":      strings.Replace(valid, "    expired_at: 2027-02-18\n", "", 1),
		"too far out":        strings.Replace(valid, "2027-02-18", "2027-06-01", 1),
		"not a date":         strings.Replace(valid, "2027-02-18", "2027-02-18T00:00:00Z", 1),
		"empty statement":    strings.Replace(valid, "    statement: >-\n      no fix,\n      dev-only.\n", "    statement: \"\"\n", 1),
		"blank block":        strings.Replace(valid, "      no fix,\n      dev-only.\n", "", 1),
		"no statement":       strings.Replace(valid, "    statement: >-\n      no fix,\n      dev-only.\n", "", 1),
		"no paths":           strings.Replace(valid, "    paths:\n      - package-lock.json\n", "", 1),
		"flow paths":         strings.Replace(valid, "    paths:\n      - package-lock.json\n", "    paths: [\"**\"]\n", 1),
		"quoted empty path":  strings.Replace(valid, "      - package-lock.json\n", "      - \"\"\n", 1),
		"glob path":          strings.Replace(valid, "      - package-lock.json\n", "      - \"**\"\n", 1),
		"commented far date": strings.Replace(valid, "2027-02-18", "2099-01-01 # renew", 1),
		"unknown key":        strings.Replace(valid, "    paths:", "    purls:\n      - pkg:npm/x\n    paths:", 1),
		"unknown section":    strings.Replace(valid, "vulnerabilities:", "ignore:", 1),
		"flow entry":         "vulnerabilities: [{id: CVE-1}]\n",
		"second unbounded":   valid + "  - id: CVE-2\n    paths:\n      - go.sum\n    statement: x\n",
		"duplicate key":      strings.Replace(valid, "    expired_at: 2027-02-18\n", "    expired_at: 2027-02-18\n    expired_at: 2099-01-01\n", 1),
		"tab-indented line":  strings.Replace(valid, "    paths:", "\tpaths:", 1),
	} {
		if err := FSGateWaiversAreBounded(text, today); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// TestOnlyTheTrivyFSGateReadsAnIgnoreFile keeps the waiver to the one gate it
// was granted for. Trivy loads `.trivyignore` and `trivy.yaml` from its working
// directory, the repository root for every scan here, so either file would
// waive findings in the SARIF scan and the image scans too; and an
// `--ignorefile` anywhere but the gate step widens the waiver the same way.
func TestOnlyTheTrivyFSGateReadsAnIgnoreFile(t *testing.T) {
	root := repoRoot(t)
	for _, name := range []string{".trivyignore", ".trivyignore.yaml", "trivy.yaml", "trivy.yml"} {
		if _, err := os.Stat(filepath.Join(root, name)); err == nil {
			t.Errorf("%s exists at the repository root, where every Trivy scan reads it by default", name)
		}
	}
	_, gate := trivyFSRunLines(t)
	files := allWorkflowFiles(t)
	actions, err := filepath.Glob(filepath.Join(root, ".github", "actions", "*", "action.y*ml"))
	if err != nil || len(actions) == 0 {
		t.Fatalf("no composite action found under .github/actions (%v) — this sweep would miss them", err)
	}
	for _, a := range actions {
		rel, _ := filepath.Rel(root, a)
		files = append(files, filepath.ToSlash(rel))
	}
	readers := 0
	for _, wf := range files {
		for line := range strings.SplitSeq(workflowfile.Read(t, wf), "\n") {
			// A Trivy config file can name an ignore file too.
			// Fail closed: a continuation line split from its `trivy` call still counts.
			trivyConfig := strings.Contains(line, "--config") && !strings.Contains(line, "gitleaks")
			if !strings.Contains(line, "--ignorefile") && !strings.Contains(line, "TRIVY_IGNOREFILE") &&
				!trivyConfig && !strings.Contains(line, "TRIVY_CONFIG") {
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			if wf == securityWorkflow && strings.TrimSpace(line) == "run: "+gate {
				readers++
				continue
			}
			t.Errorf("%s reads an ignore file outside the trivy-fs gate: %s", wf, strings.TrimSpace(line))
		}
	}
	if readers != 1 {
		t.Errorf("the trivy-fs gate line was matched %d times, want 1 — this sweep is not seeing the gate", readers)
	}
}
