package security

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mountInfoBase is the part of a container's /proc/self/mountinfo every
// fixture shares: the overlay root, procfs, the files Docker bind-mounts into
// /etc, and a data volume beside — not above — the fence directory.
const mountInfoBase = `1234 1100 0:56 / / rw,relatime master:1 - overlay overlay rw,lowerdir=/var/lib/docker/overlay2/l/A,upperdir=/var/lib/docker/overlay2/x/diff
1235 1234 0:59 / /proc rw,nosuid,nodev,noexec,relatime - proc proc rw
1240 1234 8:1 /var/lib/docker/containers/abc/resolv.conf /etc/resolv.conf rw,relatime - ext4 /dev/sda1 rw
1241 1234 8:1 /var/lib/docker/volumes/ovumcy_data/_data /app/data rw,relatime master:1 - ext4 /dev/sda1 rw
1242 1234 8:1 /var/lib/docker/volumes/other/_data /app/fen rw,relatime - ext4 /dev/sda1 rw
`

const rootDevice = 56

func sameDevice() calendarFeedFenceMountEvidence {
	return calendarFeedFenceMountEvidence{fenceDevice: rootDevice, rootDevice: rootDevice}
}

// TestCalendarFeedFenceDirectoryOnTheWritableLayerIsRefused pins WEB-506: the
// image's own /app/fence, with a data volume mounted and none at the fence,
// sits on the root's device and under no mount but "/" — the directory a
// recreation throws away. It is refused through the anchor's Read, naming the
// directory and the volume to mount, and the anchor writes nothing.
func TestCalendarFeedFenceDirectoryOnTheWritableLayerIsRefused(t *testing.T) {
	evidence := sameDevice()
	evidence.mountInfo = []byte(mountInfoBase)
	err := judgeCalendarFeedFenceMount("/app/fence", evidence)
	if !errors.Is(err, ErrCalendarFeedFenceNotMounted) {
		t.Fatalf("a fence directory on the writable layer must be refused, got %v", err)
	}
	for _, want := range []string{"/app/fence is on the container's writable layer", "-v ovumcy_fence:/app/fence"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal must say %q, got %q", want, err)
		}
	}

	dir := t.TempDir()
	fencePath := filepath.Join(dir, "calendar-feed.fence")
	var judged []string
	fence := NewCalendarFeedFenceFile(fencePath, WithCalendarFeedFenceMountCheck(func(checked string) error {
		judged = append(judged, checked)
		return judgeCalendarFeedFenceMount("/app/fence", evidence)
	}))
	for range 2 {
		if _, found, err := fence.Read(); !errors.Is(err, ErrCalendarFeedFenceNotMounted) || found {
			t.Fatalf("Read over a writable-layer directory must refuse, got found=%v err=%v", found, err)
		}
	}
	if len(judged) != 1 || judged[0] != filepath.Dir(fencePath) {
		t.Fatalf("the check must judge the fence's directory once per process, judged %q", judged)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Fatalf("a refused read must leave the fence directory untouched, found %d entries (err %v)", len(entries), err)
	}
}

// TestCalendarFeedFenceMountJudgmentAcceptsEveryMountedShape walks the shapes a
// real deployment mounts the fence with. Each accepts on ONE signal only — the
// other one says "writable layer" — so a judgment that dropped either signal
// refuses a legitimate start here.
func TestCalendarFeedFenceMountJudgmentAcceptsEveryMountedShape(t *testing.T) {
	cases := []struct {
		name      string
		dir       string
		mountLine string
		device    uint64
	}{
		{"named volume, seen by its device alone", "/app/fence", "", 2049},
		{"named volume, seen by its mountinfo entry alone", "/app/fence", "1243 1234 8:1 /var/lib/docker/volumes/ovumcy_fence/_data /app/fence rw,relatime - ext4 /dev/sda1 rw", 0},
		{"bind mount from the root's own filesystem", "/app/fence", "1243 1234 0:56 /srv/fence /app/fence rw,relatime - overlay overlay rw", 0},
		{"volume mounted at an ancestor", "/app/fence", "1243 1234 8:1 /var/lib/docker/volumes/app/_data /app rw,relatime - ext4 /dev/sda1 rw", 0},
		{"kubernetes persistent volume claim", "/app/fence", "1243 1234 252:16 / /app/fence rw,relatime - ext4 /dev/rbd0 rw", 0},
		{"Docker Desktop grpcfuse bind", "/app/fence", "1243 1234 0:120 /run/desktop/mnt/host/d/fence /app/fence rw,nosuid,nodev,relatime - fuse.grpcfuse grpcfuse rw", 0},
		{"WSL 9p bind", "/app/fence", "1243 1234 0:121 / /app/fence rw,noatime - 9p drvfs rw,aname=drvfs;path=D:\\fence", 0},
		{"mount point with an escaped space", "/app/my fence", `1243 1234 8:1 /x /app/my\040fence rw - ext4 /dev/sda1 rw`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			evidence := sameDevice()
			if tc.device != 0 {
				evidence.fenceDevice = tc.device
			}
			evidence.mountInfo = []byte(mountInfoBase + tc.mountLine + "\n")
			if err := judgeCalendarFeedFenceMount(tc.dir, evidence); err != nil {
				t.Fatalf("a mounted fence directory must be accepted, got %v", err)
			}
		})
	}
}

// TestCalendarFeedFenceMountJudgmentFailsClosedOnUnreadableEvidence pins the
// choice the judgment documents: evidence it cannot read refuses unless the
// OTHER signal still proves a mount.
func TestCalendarFeedFenceMountJudgmentFailsClosedOnUnreadableEvidence(t *testing.T) {
	unreadable := errors.New("open /proc/self/mountinfo: permission denied")

	evidence := sameDevice()
	evidence.mountInfoErr = unreadable
	err := judgeCalendarFeedFenceMount("/app/fence", evidence)
	if !errors.Is(err, ErrCalendarFeedFenceNotMounted) || !strings.Contains(err.Error(), "could not be confirmed") || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("an unreadable mountinfo over the root's device must refuse and say why, got %v", err)
	}

	evidence.fenceDevice = 2049
	if err := judgeCalendarFeedFenceMount("/app/fence", evidence); err != nil {
		t.Fatalf("a different device proves a mount without mountinfo, got %v", err)
	}

	statFailed := calendarFeedFenceMountEvidence{deviceErr: errors.New("stat /: no device number"), mountInfo: []byte(mountInfoBase)}
	if err := judgeCalendarFeedFenceMount("/app/fence", statFailed); !errors.Is(err, ErrCalendarFeedFenceNotMounted) || !strings.Contains(err.Error(), "no device number") {
		t.Fatalf("an unknown device with no covering mount must refuse, got %v", err)
	}
	statFailed.mountInfo = []byte(mountInfoBase + "1243 1234 8:1 / /app/fence rw - ext4 /dev/sda1 rw\n")
	if err := judgeCalendarFeedFenceMount("/app/fence", statFailed); err != nil {
		t.Fatalf("a covering mount proves a mount without the device, got %v", err)
	}
}

func TestUnescapeMountInfoFieldDecodesOnlyOctalEscapes(t *testing.T) {
	for raw, want := range map[string]string{
		`/plain`:            "/plain",
		`/a\040b\011c\134d`: "/a b\tc\\d",
		`/trailing\04`:      `/trailing\04`,
		`/not\08x`:          `/not\08x`,
	} {
		if got := unescapeMountInfoField(raw); got != want {
			t.Fatalf("unescapeMountInfoField(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestWithCalendarFeedFenceMountCheckIgnoresANilCheck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calendar-feed.fence")
	fence := NewCalendarFeedFenceFile(path, WithCalendarFeedFenceMountCheck(nil))
	if err := fence.Write("token"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if value, found, err := fence.Read(); err != nil || !found || value != "token" {
		t.Fatalf("a nil check must leave Read as it was, got %q found=%v err=%v", value, found, err)
	}
	accepted := NewCalendarFeedFenceFile(path, WithCalendarFeedFenceMountCheck(func(string) error { return nil }))
	if value, found, err := accepted.Read(); err != nil || !found || value != "token" {
		t.Fatalf("an accepted directory must read its token, got %q found=%v err=%v", value, found, err)
	}
}
