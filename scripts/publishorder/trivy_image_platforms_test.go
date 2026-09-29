package publishorder

import (
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// The required `trivy-image` check has to judge every platform the release
// ships, and the release's list lives in another workflow, so it cannot be read
// at run time. What can be held is the pin: each job in security.yml that
// builds the image names the one platform it builds, and those names, read off
// the source, are exactly the release's IMAGE_PLATFORMS — no more, no fewer,
// none twice. The job that carries the required context reads every other image
// job's verdict, because a job that needs `changes` may not read a job result.

const (
	securityWorkflow = ".github/workflows/security.yml"
	requiredScanJob  = "trivy-image"
	scanStepName     = "Run Trivy image scan"

	unscannedStepName = "Mark the verdict unscanned"
)

var (
	securityJobHeader = regexp.MustCompile(`(?m)^  ([A-Za-z0-9_-]+):[ \t]*$`)
	buildPlatform     = regexp.MustCompile(`(?m)^\s+platforms: (\S+)[ \t]*$`)
	jobRunner         = regexp.MustCompile(`(?m)^    runs-on: (\S+)[ \t]*$`)
	placeholder       = regexp.MustCompile(`\$\{\{[^}]*\}\}`)
	artifactName      = regexp.MustCompile(`(?m)^\s+name: (\S+)[ \t]*$`)
	artifactPath      = regexp.MustCompile(`(?m)^\s+path: (\S+)[ \t]*$`)
)

// securityJobs cuts security.yml into its jobs, in file order.
func securityJobs(content string) (names []string, blocks map[string]string) {
	_, section, _ := strings.Cut(content, "\njobs:\n")
	matches := securityJobHeader.FindAllStringSubmatchIndex(section, -1)
	blocks = map[string]string{}
	for i, m := range matches {
		end := len(section)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		name := section[m[2]:m[3]]
		names = append(names, name)
		blocks[name] = section[m[0]:end]
	}
	return names, blocks
}

// stepText is one step of a job block, from its `- name:` line to the next.
func stepText(block, name string) (string, bool) {
	marker := "      - name: " + name + "\n"
	start := strings.Index(block, marker)
	if start < 0 {
		return "", false
	}
	rest := block[start+len(marker):]
	if next := strings.Index(rest, "\n      - name: "); next >= 0 {
		rest = rest[:next]
	}
	return rest, true
}

// scanThresholdFlags are the Trivy flags that decide WHAT a scan fails on. The
// format and output flags legitimately differ between the jobs and are not
// among them.
var scanThresholdFlags = []string{"scanners", "severity", "exit-code"}

func quotedFlag(value string) string {
	if value == "" {
		return "no value"
	}
	return "`" + value + "`"
}

// trivyScanFlags reads the threshold flags off a step's `docker run … image …`
// invocation, comments left out. A severity list compares as a set. It is nil
// when the step has no `image` subcommand; a flag the invocation lacks is "".
func trivyScanFlags(step string) map[string]string {
	fields := strings.Fields(withoutComments(step))
	start := slices.Index(fields, "image")
	if start < 0 {
		return nil
	}
	flags := map[string]string{}
	for i := start + 1; i < len(fields); i++ {
		name, value, joined := strings.Cut(strings.TrimPrefix(fields[i], "--"), "=")
		if !strings.HasPrefix(fields[i], "--") || !slices.Contains(scanThresholdFlags, name) {
			continue
		}
		if !joined {
			if i+1 == len(fields) {
				break
			}
			i++
			value = fields[i]
		}
		if name == "severity" {
			parts := strings.Split(value, ",")
			slices.Sort(parts)
			value = strings.Join(parts, ",")
		}
		flags[name] = value
	}
	return flags
}

// imageCoverageProblems judges security.yml against the release's platform
// list. Every image job is found by the build action it runs, never by name.
func imageCoverageProblems(security string, release []string) []string {
	var problems []string
	names, blocks := securityJobs(security)

	covered := map[string][]string{}
	var image []string
	for _, name := range names {
		block := withoutComments(blocks[name])
		if !strings.Contains(block, "docker/build-push-action@") {
			continue
		}
		image = append(image, name)
		found := buildPlatform.FindAllStringSubmatch(block, -1)
		if len(found) != 1 || strings.Contains(found[0][1], ",") {
			problems = append(problems, "job "+name+" builds the image without exactly one named `platforms:` entry, so which platform it judges is not readable")
			continue
		}
		platform := found[0][1]
		covered[platform] = append(covered[platform], name)

		runner := jobRunner.FindStringSubmatch(block)
		_, arch, _ := strings.Cut(platform, "/")
		if runner == nil || strings.HasSuffix(runner[1], "-arm") != (arch == "arm64") {
			problems = append(problems, "job "+name+" builds "+platform+" on a runner that is not that architecture's native one, which would run the build under emulation")
		}
	}

	for _, platform := range release {
		switch len(covered[platform]) {
		case 0:
			problems = append(problems, "no image job in "+securityWorkflow+" builds "+platform+", which the release ships: it would be published unscanned by the required check")
		case 1:
		default:
			problems = append(problems, platform+" is built by "+strings.Join(covered[platform], ", ")+": one platform, one verdict")
		}
	}
	for platform, jobs := range covered {
		if !slices.Contains(release, platform) {
			problems = append(problems, strings.Join(jobs, ", ")+" builds "+platform+", which the release does not ship")
		}
	}

	gate, ok := blocks[requiredScanJob]
	if !ok || !slices.Contains(image, requiredScanJob) {
		return append(problems, "job "+requiredScanJob+" no longer builds the image itself")
	}
	gateScan, _ := stepText(gate, scanStepName)
	gateFlags := trivyScanFlags(gateScan)
	if gateFlags == nil {
		problems = append(problems, requiredScanJob+" has no `image` scan in its step "+scanStepName+", so there is no threshold to hold the other jobs to")
	}
	for _, name := range image {
		if name == requiredScanJob {
			continue
		}
		if gateFlags != nil {
			scan, _ := stepText(blocks[name], scanStepName)
			flags := trivyScanFlags(scan)
			if flags == nil {
				problems = append(problems, name+" has no `image` scan in its step "+scanStepName)
			}
			for _, flag := range scanThresholdFlags {
				if flags != nil && flags[flag] != gateFlags[flag] {
					problems = append(problems, name+" scans with `--"+flag+"` "+quotedFlag(flags[flag])+", "+requiredScanJob+" with "+quotedFlag(gateFlags[flag])+": the two scans judge different findings")
				}
			}
		}
		verdict := "verdict-" + name
		if !strings.Contains(gate, "\n      - "+name+"\n") {
			problems = append(problems, requiredScanJob+" does not need "+name+", so its scan is not part of the required check")
		}
		upload, _ := stepText(blocks[name], "Hand the verdict to trivy-image")
		if !strings.Contains(upload, "name: "+verdict+"\n") || !strings.Contains(upload, "overwrite: true") {
			problems = append(problems, name+" does not hand its verdict over as "+verdict+" with `overwrite: true`; a re-run would leave the old red in place")
		}
		if !strings.Contains(gate, "actions/download-artifact@") || !strings.Contains(gate, "name: "+verdict+"\n") {
			problems = append(problems, requiredScanJob+" does not download "+verdict)
		}
	}
	return problems
}

func releasePlatforms(t *testing.T) []string {
	t.Helper()
	return strings.Split(jobEnv(workflowfile.Job(t, publishWorkflow, publishJob))[platformsKey], ",")
}

func TestTrivyImageJudgesEveryPlatformTheReleaseShips(t *testing.T) {
	release := releasePlatforms(t)
	if len(release) < 2 {
		t.Fatalf("%s is %v: this guard exists for a multi-platform release", platformsKey, release)
	}
	if problems := imageCoverageProblems(workflowfile.Read(t, securityWorkflow), release); len(problems) > 0 {
		t.Errorf("%s no longer judges what %s ships:\n%s", securityWorkflow, publishWorkflow, strings.Join(problems, "\n"))
	}
}

// inJob replaces the first `old` inside one job's block, so a drift lands in
// the job the case names and not in an earlier job that spells it the same.
func inJob(t *testing.T, content, job, old, replacement string) string {
	t.Helper()
	_, blocks := securityJobs(content)
	block, ok := blocks[job]
	if !ok || !strings.Contains(block, old) {
		t.Fatalf("job %q has no %q to change", job, old)
	}
	return strings.Replace(content, block, strings.Replace(block, old, replacement, 1), 1)
}

// TestTrivyImageCoverageGuardGoesRedWhenTheListsDrift feeds the guard the
// drifts it exists for, so a check that read nothing would fail here.
func TestTrivyImageCoverageGuardGoesRedWhenTheListsDrift(t *testing.T) {
	security := workflowfile.Read(t, securityWorkflow)
	release := releasePlatforms(t)

	for _, testCase := range []struct {
		name     string
		security string
		release  []string
		want     string
	}{
		{"the arm64 job builds amd64 instead", strings.Replace(security, "platforms: linux/arm64", "platforms: linux/amd64", 1), release, "no image job"},
		{"the release gains a platform", security, append(slices.Clone(release), "linux/riscv64"), "linux/riscv64, which the release ships"},
		{"the release drops a platform", security, []string{"linux/amd64"}, "which the release does not ship"},
		{"the arm64 build runs under emulation", strings.Replace(security, "runs-on: ubuntu-24.04-arm", "runs-on: ubuntu-latest", 1), release, "native one"},
		{"trivy-image stops waiting for the arm64 job", strings.Replace(security, "      - trivy-image-arm64\n", "", 1), release, "does not need trivy-image-arm64"},
		{"the arm64 job forgets overwrite", strings.Replace(security, "overwrite: true", "overwrite: false", 1), release, "overwrite: true"},
		{"the arm64 scan narrows the severity", inJob(t, security, "trivy-image-arm64", "--severity HIGH,CRITICAL", "--severity CRITICAL"), release, "`--severity` `CRITICAL`"},
		{"the arm64 scan drops the exit code", inJob(t, security, "trivy-image-arm64", " --exit-code 1", ""), release, "`--exit-code` no value"},
		{"the arm64 scan changes the scanners", inJob(t, security, "trivy-image-arm64", "--scanners vuln", "--scanners vuln,secret"), release, "`--scanners`"},
		{"the required job's own threshold moves", inJob(t, security, "trivy-image", "--severity HIGH,CRITICAL", "--severity CRITICAL"), release, "`--severity`"},
		{"the arm64 scan is no longer an image scan", inJob(t, security, "trivy-image-arm64", " image --scanners", " fs --scanners"), release, "has no `image` scan"},
		{"trivy-image reads another artifact", strings.Replace(security, "          name: verdict-trivy-image-arm64\n          path: .tmp/security/arm64", "          name: verdict-other\n          path: .tmp/security/arm64", 1), release, "does not download"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.security == security && slices.Equal(testCase.release, release) {
				t.Fatal("the case changes nothing")
			}
			problems := strings.Join(imageCoverageProblems(testCase.security, testCase.release), "\n")
			if !strings.Contains(problems, testCase.want) {
				t.Errorf("the guard answered %q, want a problem naming %q", problems, testCase.want)
			}
		})
	}
	if problems := imageCoverageProblems(security, release); len(problems) > 0 {
		t.Errorf("the guard refuses the real workflow, so the cases above prove nothing: %v", problems)
	}
}

// runStepIn runs one step's `run:` block the way the runner does (bash -e), in
// a directory the caller keeps so two steps of one job can run one after the
// other over the same workspace, with `docker` shadowed by the stub.
func runStepIn(t *testing.T, bash, dir, block, stub string) error {
	t.Helper()

	marker := "        run: |\n"
	start := strings.Index(block, marker)
	if start < 0 {
		t.Fatalf("no `run: |` block in:\n%s", block)
	}
	var script []string
	for _, line := range strings.Split(block[start+len(marker):], "\n") {
		script = append(script, strings.TrimPrefix(line, "          "))
	}
	body := placeholder.ReplaceAllString(strings.Join(script, "\n"), "x")

	file := filepath.Join(dir, "step.sh")
	if err := os.WriteFile(file, []byte(stub+body), 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(bash, "-e", filepath.ToSlash(file))
	command.Dir = dir
	out, err := command.CombinedOutput()
	t.Logf("%s", out)
	return err
}

// The verdict travels as a file: the scan writes it where the upload reads it,
// and the required job judges it where the download puts it. Each leg of that
// path is read off the workflow and run, so a drift in any of them fails here.
func TestTheArm64VerdictTravelsFromTheScanToTheRequiredCheck(t *testing.T) {
	bash := requireBash(t)
	_, blocks := securityJobs(workflowfile.Read(t, securityWorkflow))
	release := releasePlatforms(t)
	gate := blocks[requiredScanJob]

	for _, platform := range release {
		_, arch, _ := strings.Cut(platform, "/")
		if arch == "amd64" {
			continue // scanned in the required job itself, no hand-over
		}
		helper := requiredScanJob + "-" + arch
		block, ok := blocks[helper]
		if !ok {
			t.Fatalf("no job %s for %s", helper, platform)
		}

		scan, ok := stepText(block, scanStepName)
		if !ok {
			t.Fatalf("%s has no step %q", helper, scanStepName)
		}
		upload, _ := stepText(block, "Hand the verdict to trivy-image")
		uploaded := artifactPath.FindStringSubmatch(upload)
		fetch, _ := stepText(gate, "Fetch the "+platform+" verdict")
		fetched := artifactPath.FindStringSubmatch(fetch)
		judge, ok := stepText(gate, "Judge the "+platform+" verdict")
		if uploaded == nil || fetched == nil || !ok {
			t.Fatalf("%s or %s lacks the upload/fetch/judge steps for %s", helper, requiredScanJob, platform)
		}
		if got := artifactName.FindStringSubmatch(upload); got == nil || got[1] != "verdict-"+helper {
			t.Fatalf("%s uploads %v, want verdict-%s", helper, got, helper)
		}
		if found := workflowfile.StepFailOpenKeys(scan); len(found) > 0 {
			t.Errorf("%s's scan carries %q: a finding would no longer fail it", helper, found)
		}
		for _, step := range []string{fetch, judge} {
			if found := workflowfile.StepFailOpenKeys(step); !slices.Equal(found, []string{"if: always()"}) {
				t.Errorf("a %s hand-over step in %s carries %q, want only `if: always()`: the verdict is owed even when the amd64 scan failed", platform, requiredScanJob, found)
			}
		}
		if found := workflowfile.StepFailOpenKeys(upload); !slices.Equal(found, []string{"if: always()"}) {
			t.Errorf("%s's upload carries %q, want only `if: always()`: an attempt that stopped early must still replace the last attempt's verdict", helper, found)
		}

		verdictFile := path.Base(uploaded[1])

		// An attempt that dies before the scan (checkout, login, build) has
		// written nothing of its own, and the artifact of the attempt before it
		// is still in the run. So the job's first step leaves a verdict that
		// no exit code can equal, at the path the upload reads.
		unscanned, ok := stepText(block, unscannedStepName)
		if !ok || !strings.Contains(block, "    steps:\n      - name: "+unscannedStepName+"\n") {
			t.Fatalf("%s does not start with the step %q: a failure before its scan would leave the previous attempt's verdict in place", helper, unscannedStepName)
		}
		if found := workflowfile.StepFailOpenKeys(unscanned); len(found) > 0 {
			t.Errorf("%s's %q carries %q: the placeholder must be written on every attempt", helper, unscannedStepName, found)
		}
		placeholderDir := t.TempDir()
		if err := runStepIn(t, bash, placeholderDir, unscanned, ""); err != nil {
			t.Fatalf("%s's %q failed: %v", helper, unscannedStepName, err)
		}
		left, readErr := os.ReadFile(filepath.Join(placeholderDir, filepath.FromSlash(uploaded[1])))
		if readErr != nil || strings.TrimSpace(string(left)) == "" || strings.TrimSpace(string(left)) == "0" {
			t.Fatalf("%q wrote %q (%v) to the path the upload reads, %s, want a non-empty verdict that is not 0", unscannedStepName, left, readErr, uploaded[1])
		}
		t.Run(helper+"/the placeholder", func(t *testing.T) {
			judgeDir := t.TempDir()
			target := filepath.Join(judgeDir, filepath.FromSlash(fetched[1]), verdictFile)
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(target, left, 0o644); err != nil {
				t.Fatal(err)
			}
			if judged, err := judgeIn(t, bash, judge, judgeDir); err == nil {
				t.Fatalf("the judge step passed the placeholder verdict %q:\n%s", left, judged)
			}
		})
		for _, testCase := range []struct {
			name      string
			exit      string
			wantScan  bool
			wantJudge bool
		}{
			{"clean", "0", true, true},
			{"a finding", "1", false, false},
			{"an error exit", "2", false, false},
		} {
			t.Run(helper+"/"+testCase.name, func(t *testing.T) {
				stub := "docker() { return " + testCase.exit + "; }\n"
				// The placeholder is already in the workspace when the scan runs.
				dir := t.TempDir()
				if err := runStepIn(t, bash, dir, unscanned, ""); err != nil {
					t.Fatal(err)
				}
				err := runStepIn(t, bash, dir, scan, stub)
				if (err == nil) != testCase.wantScan {
					t.Fatalf("the scan step exited %v for Trivy exit %s, want pass=%v", err, testCase.exit, testCase.wantScan)
				}
				written, readErr := os.ReadFile(filepath.Join(dir, filepath.FromSlash(uploaded[1])))
				if readErr != nil || strings.TrimSpace(string(written)) != testCase.exit {
					t.Fatalf("the scan wrote verdict %q (%v) to the path the upload reads, %s, want %s", written, readErr, uploaded[1], testCase.exit)
				}

				// The download puts the artifact's file under its `path:`.
				judgeDir := t.TempDir()
				target := filepath.Join(judgeDir, filepath.FromSlash(fetched[1]), verdictFile)
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(target, written, 0o644); err != nil {
					t.Fatal(err)
				}
				judged, judgeErr := judgeIn(t, bash, judge, judgeDir)
				if (judgeErr == nil) != testCase.wantJudge {
					t.Fatalf("the judge step exited %v for verdict %s, want pass=%v:\n%s", judgeErr, testCase.exit, testCase.wantJudge, judged)
				}
			})
		}

		for _, bad := range []struct {
			name    string
			content *string
		}{
			{"a missing verdict", nil},
			{"an empty verdict", new("")},
			{"a verdict that only starts with 0", new("00\n")},
		} {
			t.Run(helper+"/"+bad.name, func(t *testing.T) {
				judgeDir := t.TempDir()
				if bad.content != nil {
					target := filepath.Join(judgeDir, filepath.FromSlash(fetched[1]), verdictFile)
					if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(target, []byte(*bad.content), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if judged, err := judgeIn(t, bash, judge, judgeDir); err == nil {
					t.Fatalf("the judge step passed with %s:\n%s", bad.name, judged)
				}
			})
		}
	}
}

func judgeIn(t *testing.T, bash, judge, dir string) (string, error) {
	t.Helper()

	marker := "        run: |\n"
	start := strings.Index(judge, marker)
	if start < 0 {
		t.Fatalf("no `run: |` block in the judge step:\n%s", judge)
	}
	var script []string
	for _, line := range strings.Split(judge[start+len(marker):], "\n") {
		script = append(script, strings.TrimPrefix(line, "          "))
	}
	file := filepath.Join(t.TempDir(), "judge.sh")
	if err := os.WriteFile(file, []byte(strings.Join(script, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(bash, "-e", filepath.ToSlash(file))
	command.Dir = dir
	out, err := command.CombinedOutput()
	return string(out), err
}
