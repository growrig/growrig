package camera

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/growrig/growrig/growcore/internal/domain"
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
