package camera

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/growrig/growrig/growcore/internal/domain"
	"github.com/growrig/growrig/growcore/internal/store"
)

func testRecorder(t *testing.T) *Recorder {
	t.Helper()
	return &Recorder{
		root:        t.TempDir(),
		subscribers: map[string]map[chan []byte]struct{}{},
		stats:       map[string]*streamStats{},
	}
}

func fakeFFmpeg(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ffmpeg")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func withCaptureTimeouts(t *testing.T, startup, stall time.Duration) {
	t.Helper()
	oldStartup, oldStall := frameStartupTimeout, frameStallTimeout
	frameStartupTimeout, frameStallTimeout = startup, stall
	t.Cleanup(func() { frameStartupTimeout, frameStallTimeout = oldStartup, oldStall })
}

func TestCaptureWatchdogStopsStreamThatNeverProducesAFrame(t *testing.T) {
	fakeFFmpeg(t, "exec /bin/sleep 30")
	withCaptureTimeouts(t, 50*time.Millisecond, 50*time.Millisecond)
	r := testRecorder(t)

	received, err := r.capture(context.Background(), domain.Binding{ID: "cam", EnvironmentID: "env", StreamURL: "rtsp://camera/live"})

	if received {
		t.Fatal("reported a frame from a silent stream")
	}
	if err == nil || !strings.Contains(err.Error(), "waiting for first frame") {
		t.Fatalf("expected first-frame watchdog error, got %v", err)
	}
}

func TestCaptureWatchdogStopsStreamAfterFramesStall(t *testing.T) {
	fakeFFmpeg(t, "printf '\\377\\330\\377\\331'; exec /bin/sleep 30")
	withCaptureTimeouts(t, time.Second, 50*time.Millisecond)
	r := testRecorder(t)

	received, err := r.capture(context.Background(), domain.Binding{ID: "cam", EnvironmentID: "env", StreamURL: "rtsp://camera/live", CameraCaptureInterval: 60})

	if !received {
		t.Fatal("did not report the frame produced before the stall")
	}
	if err == nil || !strings.Contains(err.Error(), "without a new frame") {
		t.Fatalf("expected stalled-frame watchdog error, got %v", err)
	}
	if stats := r.StreamStats("cam"); !stats.Online || stats.Status != "online" {
		t.Fatalf("expected recorded frame health, got %+v", stats)
	}
}

func TestStreamStatsExposeReconnectStateAndError(t *testing.T) {
	r := testRecorder(t)
	r.markConnecting("cam", 3, context.DeadlineExceeded)

	stats := r.StreamStats("cam")
	if stats.Online || stats.Status != "reconnecting" || stats.RetryCount != 3 {
		t.Fatalf("unexpected reconnect stats: %+v", stats)
	}
	if !strings.Contains(stats.LastError, "deadline exceeded") {
		t.Fatalf("missing reconnect error: %+v", stats)
	}
}

// writeSnapshot plants an archived frame whose filename encodes `at`, with an
// mtime of now — the shape an rsynced or restored archive has on disk.
func writeSnapshot(t *testing.T, r *Recorder, b domain.Binding, at time.Time, size int) string {
	t.Helper()
	dir := filepath.Join(r.cameraDir(b.EnvironmentID, b.ID), at.Format("2006/01/02"))
	if err := os.MkdirAll(dir, 0750); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, at.Format("20060102T150405.000")+".jpg")
	if err := os.WriteFile(path, make([]byte, size), 0640); err != nil {
		t.Fatal(err)
	}
	return path
}

func cameraBinding() domain.Binding {
	return domain.Binding{ID: "cam", EnvironmentID: "env", Name: "Tent Camera", Kind: domain.KindCamera, CameraType: domain.CameraRTSP}
}

func TestPruneUsesCaptureTimeNotFileModTime(t *testing.T) {
	r := testRecorder(t)
	b := cameraBinding()
	b.CameraRetentionDays = 7
	b.CameraRetentionPolicy = domain.RetentionDelete
	now := time.Now()
	old := writeSnapshot(t, r, b, now.AddDate(0, 0, -30), 1024)
	recent := writeSnapshot(t, r, b, now.AddDate(0, 0, -1), 1024)

	if err := r.prune(b, now); err != nil {
		t.Fatal(err)
	}

	// Both files were just written, so mtime says "new" for each; only the
	// capture time in the filename distinguishes them.
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Errorf("30-day-old snapshot survived retention: %v", err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Errorf("yesterday's snapshot was deleted: %v", err)
	}
}

func TestPruneRemovesEmptyDayDirectories(t *testing.T) {
	r := testRecorder(t)
	b := cameraBinding()
	b.CameraRetentionDays = 2
	b.CameraRetentionPolicy = domain.RetentionDelete
	now := time.Now()
	stale := now.AddDate(0, 0, -20)
	writeSnapshot(t, r, b, stale, 512)
	writeSnapshot(t, r, b, now, 512)

	if err := r.prune(b, now); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(r.cameraDir(b.EnvironmentID, b.ID), stale.Format("2006/01/02"))
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("emptied day directory %s was left behind: %v", dir, err)
	}
}

func TestPruneDropsOldestFirstOverStorageLimit(t *testing.T) {
	r := testRecorder(t)
	b := cameraBinding()
	b.CameraStorageMB = 1
	b.CameraRetentionPolicy = domain.RetentionDelete
	now := time.Now()
	// 3 × 400 KB exceeds the 1 MB limit; dropping the oldest brings it back under.
	oldest := writeSnapshot(t, r, b, now.Add(-3*time.Hour), 400<<10)
	middle := writeSnapshot(t, r, b, now.Add(-2*time.Hour), 400<<10)
	newest := writeSnapshot(t, r, b, now.Add(-time.Hour), 400<<10)

	if err := r.prune(b, now); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(oldest); !os.IsNotExist(err) {
		t.Error("oldest snapshot should have been dropped first")
	}
	for _, keep := range []string{middle, newest} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("snapshot %s should have been kept: %v", keep, err)
		}
	}
}

func TestUsageReportsLimitsWithoutDeleting(t *testing.T) {
	r := testRecorder(t)
	b := cameraBinding()
	b.CameraRetentionDays = 7
	b.CameraStorageMB = 1
	now := time.Now()
	old := writeSnapshot(t, r, b, now.AddDate(0, 0, -30), 800<<10)
	writeSnapshot(t, r, b, now, 800<<10)

	usage, err := r.Usage(b)
	if err != nil {
		t.Fatal(err)
	}

	if !usage.OverAge || !usage.OverStorage {
		t.Errorf("expected both limits flagged, got age=%v storage=%v", usage.OverAge, usage.OverStorage)
	}
	if usage.Snapshots != 2 || usage.Bytes != 1600<<10 {
		t.Errorf("got %d snapshots / %d bytes, want 2 / %d", usage.Snapshots, usage.Bytes, 1600<<10)
	}
	if usage.Policy != domain.RetentionAlert {
		t.Errorf("policy defaulted to %q, want alert", usage.Policy)
	}
	if _, err := os.Stat(old); err != nil {
		t.Errorf("measuring usage deleted a snapshot: %v", err)
	}
}

func TestUsageTreatsZeroLimitsAsUnlimited(t *testing.T) {
	r := testRecorder(t)
	b := cameraBinding()
	writeSnapshot(t, r, b, time.Now().AddDate(-2, 0, 0), 4<<20)

	usage, err := r.Usage(b)
	if err != nil {
		t.Fatal(err)
	}

	if usage.OverAge || usage.OverStorage {
		t.Errorf("unlimited archive reported over limit: age=%v storage=%v", usage.OverAge, usage.OverStorage)
	}
}

func TestSaveKeepsSnapshotsPastTheLimits(t *testing.T) {
	r := testRecorder(t)
	b := cameraBinding()
	b.CameraRetentionDays = 1
	b.CameraStorageMB = 1
	old := writeSnapshot(t, r, b, time.Now().AddDate(0, 0, -10), 2<<20)

	if err := r.save(b, []byte{0xff, 0xd8, 0xff, 0xd9}, time.Now()); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(old); err != nil {
		t.Errorf("capturing a frame deleted archived footage: %v", err)
	}
}

func TestEnforceRetentionAlertsInsteadOfDeleting(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	r := testRecorder(t)
	r.store = st

	b := cameraBinding()
	b.StreamURL = "rtsp://camera/live"
	b.CameraStorageMB = 1
	if err := st.SaveEnvironment(domain.Environment{ID: b.EnvironmentID, Name: "Tent", Kind: domain.KindTent}); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveBinding(b); err != nil {
		t.Fatal(err)
	}
	kept := writeSnapshot(t, r, b, time.Now().Add(-time.Hour), 2<<20)

	r.enforceRetention()

	if _, err := os.Stat(kept); err != nil {
		t.Fatalf("alert policy deleted a snapshot: %v", err)
	}
	alerts, err := st.OpenAlerts()
	if err != nil {
		t.Fatal(err)
	}
	var found *domain.Alert
	for i := range alerts {
		if alerts[i].Kind == "camera-storage" {
			found = &alerts[i]
		}
	}
	if found == nil {
		t.Fatalf("no storage alert opened, got %d alert(s)", len(alerts))
	}
	if found.EnvironmentID != b.EnvironmentID {
		t.Errorf("alert environment = %q, want %q", found.EnvironmentID, b.EnvironmentID)
	}

	// Raising the limit past actual usage should clear the alert again.
	b.CameraStorageMB = 100
	if err := st.SaveBinding(b); err != nil {
		t.Fatal(err)
	}
	r.enforceRetention()
	alerts, err = st.OpenAlerts()
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range alerts {
		if a.Kind == "camera-storage" {
			t.Error("storage alert stayed open after the limit was raised")
		}
	}
}
