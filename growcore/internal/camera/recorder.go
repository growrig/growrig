// Package camera maintains persistent RTSP connections and a rolling JPEG archive.
package camera

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/growrig/growrig/growcore/internal/domain"
	"github.com/growrig/growrig/growcore/internal/store"
)

type Recorder struct {
	store       *store.Store
	root        string
	legacyRoot  string
	mu          sync.Mutex
	workers     map[string]workerState
	subscribers map[string]map[chan []byte]struct{}
	observed    map[string]string
	stats       map[string]*streamStats
}

type streamStats struct {
	windowStart               time.Time
	windowBytes, windowFrames int64
	bitrateBps                int64
	fps                       float64
	lastFrame                 time.Time
	status                    string
	retryCount                int
	lastError                 string
	statusSince               time.Time
}
type Stats struct {
	BitrateBps  int64     `json:"bitrateBps"`
	FPS         float64   `json:"fps"`
	Online      bool      `json:"online"`
	LastFrame   time.Time `json:"lastFrame,omitempty"`
	Status      string    `json:"status"`
	RetryCount  int       `json:"retryCount"`
	LastError   string    `json:"lastError,omitempty"`
	StatusSince time.Time `json:"statusSince,omitempty"`
}

const streamOnlineWindow = 10 * time.Second

var (
	frameStartupTimeout = 20 * time.Second
	frameStallTimeout   = 15 * time.Second
	retentionInterval   = 5 * time.Minute
)

type workerState struct {
	cancel    context.CancelFunc
	signature string
}

type Snapshot struct {
	ID   string    `json:"id"`
	Time time.Time `json:"time"`
}

// SnapshotPage is a (possibly downsampled) window of the archive; Total counts
// every snapshot in the window before downsampling.
type SnapshotPage struct {
	Snapshots []Snapshot `json:"snapshots"`
	Total     int        `json:"total"`
}

// New builds a recorder that archives under environmentsDir. legacyDBPath is the
// database file path, used only to locate and migrate pre-unified-data-dir
// archives (which lived at "<db>.cameras").
func New(st *store.Store, environmentsDir, legacyDBPath string) *Recorder {
	return &Recorder{store: st, root: environmentsDir, legacyRoot: legacyDBPath + ".cameras", workers: map[string]workerState{}, subscribers: map[string]map[chan []byte]struct{}{}, observed: map[string]string{}, stats: map[string]*streamStats{}}
}

func (r *Recorder) StreamStats(id string) Stats {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stats[id]
	if s == nil {
		return Stats{Status: "connecting"}
	}
	online := s.status == "online" && time.Since(s.lastFrame) < streamOnlineWindow
	status := s.status
	if status == "online" && !online {
		status = "stalled"
	}
	return Stats{BitrateBps: s.bitrateBps, FPS: s.fps, Online: online, LastFrame: s.lastFrame, Status: status, RetryCount: s.retryCount, LastError: s.lastError, StatusSince: s.statusSince}
}

func (r *Recorder) recordFrame(id string, bytes int) {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stats[id]
	if s == nil {
		s = &streamStats{windowStart: now}
		r.stats[id] = s
	}
	s.windowBytes += int64(bytes)
	s.windowFrames++
	s.lastFrame = now
	if s.status != "online" {
		s.statusSince = now
	}
	s.status = "online"
	s.retryCount = 0
	s.lastError = ""
	if elapsed := now.Sub(s.windowStart); elapsed >= time.Second {
		s.bitrateBps = int64(float64(s.windowBytes*8) / elapsed.Seconds())
		s.fps = float64(s.windowFrames) / elapsed.Seconds()
		s.windowBytes = 0
		s.windowFrames = 0
		s.windowStart = now
	}
}

func (r *Recorder) markConnecting(id string, retryCount int, lastError error) {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.stats[id]
	if s == nil {
		s = &streamStats{windowStart: now}
		r.stats[id] = s
	}
	if retryCount == 0 {
		s.status = "connecting"
	} else {
		s.status = "reconnecting"
	}
	s.retryCount = retryCount
	if lastError != nil {
		s.lastError = lastError.Error()
	}
	s.statusSince = now
}

func (r *Recorder) Run(ctx context.Context) {
	log.Printf("camera recorder: starting (archive=%s, ffmpeg=%s)", r.root, ffmpegPath())
	r.migrateLegacyArchive()
	r.reconcile(ctx)
	r.enforceRetention()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	// Measuring an archive walks every file, so it runs on its own slow ticker
	// rather than on each capture the way the old delete-on-save pass did.
	retention := time.NewTicker(retentionInterval)
	defer retention.Stop()
	for {
		select {
		case <-ctx.Done():
			r.stopAll()
			return
		case <-ticker.C:
			r.reconcile(ctx)
		case <-retention.C:
			r.enforceRetention()
		}
	}
}

func (r *Recorder) cameraDir(environmentID, id string) string {
	return filepath.Join(r.root, environmentID, "cameras", id)
}
func (r *Recorder) Latest(environmentID, id string) string {
	return filepath.Join(r.cameraDir(environmentID, id), "latest.jpg")
}

func (r *Recorder) Snapshots(environmentID, id string, limit int) ([]Snapshot, error) {
	page, err := r.SnapshotRange(environmentID, id, time.Time{}, time.Time{}, limit)
	return page.Snapshots, err
}

// SnapshotRange lists snapshots captured within [since, until] (zero bounds are
// open ended), newest first. When more snapshots exist than limit, the range is
// split into limit evenly spaced buckets and the newest snapshot of each bucket
// is returned, so a timeline keeps a representative frame per pixel column
// instead of only the most recent slice of the window.
func (r *Recorder) SnapshotRange(environmentID, id string, since, until time.Time, limit int) (SnapshotPage, error) {
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	var snapshots []Snapshot
	err := filepath.WalkDir(r.cameraDir(environmentID, id), func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipArchiveDir(r.cameraDir(environmentID, id), path, since, until) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Base(path) == "latest.jpg" || filepath.Ext(path) != ".jpg" {
			return nil
		}
		stamp := filepath.Base(path[:len(path)-len(filepath.Ext(path))])
		at, err := time.ParseInLocation("20060102T150405.000", stamp, time.Local)
		if err != nil {
			return nil
		}
		if (!since.IsZero() && at.Before(since)) || (!until.IsZero() && at.After(until)) {
			return nil
		}
		snapshots = append(snapshots, Snapshot{ID: stamp, Time: at})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return SnapshotPage{}, err
	}
	sort.Slice(snapshots, func(i, j int) bool { return snapshots[i].Time.After(snapshots[j].Time) })
	page := SnapshotPage{Total: len(snapshots), Snapshots: snapshots}
	if len(snapshots) > limit {
		if since.IsZero() || until.IsZero() {
			page.Snapshots = snapshots[:limit]
		} else {
			page.Snapshots = downsample(snapshots, since, until, limit)
		}
	}
	if page.Snapshots == nil {
		page.Snapshots = []Snapshot{}
	}
	return page, nil
}

// downsample keeps the newest snapshot of each of limit evenly spaced buckets
// spanning [since, until]. Input must be sorted newest first.
func downsample(snapshots []Snapshot, since, until time.Time, limit int) []Snapshot {
	span := until.Sub(since)
	if span <= 0 {
		return snapshots[:limit]
	}
	kept := make([]Snapshot, 0, limit)
	previous := -1
	for _, s := range snapshots {
		bucket := int(float64(s.Time.Sub(since)) / float64(span) * float64(limit))
		if bucket >= limit {
			bucket = limit - 1
		}
		if bucket == previous {
			continue
		}
		previous = bucket
		kept = append(kept, s)
	}
	return kept
}

// skipArchiveDir reports whether a YYYY, YYYY/MM or YYYY/MM/DD archive
// directory falls entirely outside the requested window.
func skipArchiveDir(root, path string, since, until time.Time) bool {
	if since.IsZero() && until.IsZero() {
		return false
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == "." {
		return false
	}
	parts := strings.Split(relative, string(filepath.Separator))
	if len(parts) > 3 {
		return false
	}
	layouts := []string{"2006", "2006/01", "2006/01/02"}
	start, err := time.ParseInLocation(layouts[len(parts)-1], strings.Join(parts, "/"), time.Local)
	if err != nil {
		return false
	}
	var end time.Time
	switch len(parts) {
	case 1:
		end = start.AddDate(1, 0, 0)
	case 2:
		end = start.AddDate(0, 1, 0)
	default:
		end = start.AddDate(0, 0, 1)
	}
	return (!since.IsZero() && !end.After(since)) || (!until.IsZero() && start.After(until))
}

func (r *Recorder) SnapshotPath(environmentID, id, stamp string) (string, error) {
	at, err := time.ParseInLocation("20060102T150405.000", stamp, time.Local)
	if err != nil {
		return "", errors.New("invalid snapshot id")
	}
	path := filepath.Join(r.cameraDir(environmentID, id), at.Format("2006/01/02"), stamp+".jpg")
	if _, err := os.Stat(path); err != nil {
		// Compatibility with archives created before snapshots were grouped by
		// day instead of by hour.
		legacyPath := filepath.Join(r.cameraDir(environmentID, id), at.Format("2006/01/02/15"), stamp+".jpg")
		if _, legacyErr := os.Stat(legacyPath); legacyErr != nil {
			return "", err
		}
		return legacyPath, nil
	}
	return path, nil
}

// Subscribe receives frames decoded by the persistent worker. Slow clients
// drop frames instead of delaying recording or opening another RTSP session.
func (r *Recorder) Subscribe(id string) (<-chan []byte, func()) {
	ch := make(chan []byte, 1)
	r.mu.Lock()
	if r.subscribers[id] == nil {
		r.subscribers[id] = map[chan []byte]struct{}{}
	}
	r.subscribers[id][ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		delete(r.subscribers[id], ch)
		if len(r.subscribers[id]) == 0 {
			delete(r.subscribers, id)
		}
		r.mu.Unlock()
	}
}

func (r *Recorder) publish(id string, frame []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for ch := range r.subscribers[id] {
		select {
		case ch <- frame:
		default:
		}
	}
}

func (r *Recorder) reconcile(parent context.Context) {
	bindings, err := r.store.Bindings()
	if err != nil {
		log.Printf("camera recorder: list bindings: %v", err)
		return
	}
	wanted := map[string]domain.Binding{}
	for _, b := range bindings {
		if b.Kind == domain.KindCamera {
			diagnostic := fmt.Sprintf("type=%q entity=%t stream=%s interval=%ds retention=%dd storage=%dMB", b.CameraType, b.Entity != "", streamDescription(b.StreamURL), b.CameraCaptureInterval, b.CameraRetentionDays, b.CameraStorageMB)
			if r.observed[b.ID] != diagnostic {
				log.Printf("camera recorder %s (%s): binding %s", b.Name, b.ID, diagnostic)
				r.observed[b.ID] = diagnostic
			}
		}
		if b.Kind == domain.KindCamera && b.CameraType == domain.CameraRTSP && b.StreamURL != "" {
			wanted[b.ID] = b
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, worker := range r.workers {
		b, ok := wanted[id]
		if !ok || worker.signature != signature(b) {
			log.Printf("camera recorder %s: stopping worker (binding removed or changed)", id)
			worker.cancel()
			delete(r.workers, id)
		}
	}
	for id, b := range wanted {
		if _, ok := r.workers[id]; ok {
			continue
		}
		ctx, cancel := context.WithCancel(parent)
		r.workers[id] = workerState{cancel: cancel, signature: signature(b)}
		log.Printf("camera recorder %s (%s): starting persistent RTSP worker (%s)", b.Name, id, streamDescription(b.StreamURL))
		go r.worker(ctx, b)
	}
}

func (r *Recorder) stopAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for id, worker := range r.workers {
		worker.cancel()
		delete(r.workers, id)
	}
}

func signature(b domain.Binding) string {
	return fmt.Sprintf("%s|%d|%d|%d", b.StreamURL, b.CameraCaptureInterval, b.CameraRetentionDays, b.CameraStorageMB)
}

func (r *Recorder) worker(ctx context.Context, b domain.Binding) {
	backoff := time.Second
	retryCount := 0
	for ctx.Err() == nil {
		r.markConnecting(b.ID, retryCount, nil)
		receivedFrame, err := r.capture(ctx, b)
		if ctx.Err() != nil {
			return
		}
		if receivedFrame {
			backoff = time.Second
			retryCount = 0
		}
		retryCount++
		r.markConnecting(b.ID, retryCount, err)
		log.Printf("camera recorder %s: %v; reconnecting in %s", b.Name, err, backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

type frameResult struct {
	jpeg []byte
	err  error
}

func (r *Recorder) capture(ctx context.Context, b domain.Binding) (bool, error) {
	interval := b.CameraCaptureInterval
	if interval <= 0 {
		interval = 60
	}
	started := time.Now()
	log.Printf("camera recorder %s: launching ffmpeg for %s", b.ID, streamDescription(b.StreamURL))
	cmd := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-loglevel", "warning", "-nostdin",
		"-rtsp_transport", "tcp", "-i", b.StreamURL, "-map", "0:v:0", "-an",
		"-vf", "fps=5", "-c:v", "mjpeg", "-q:v", "5", "-f", "image2pipe", "pipe:1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return false, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return false, fmt.Errorf("open ffmpeg stderr: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return false, fmt.Errorf("start ffmpeg: %w", err)
	}
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			log.Printf("camera recorder %s: ffmpeg: %s", b.ID, sanitizeFFmpegLine(scanner.Text(), b.StreamURL))
		}
	}()
	readCtx, cancelRead := context.WithCancel(ctx)
	defer cancelRead()
	frames := make(chan frameResult, 1)
	go func() {
		reader := bufio.NewReaderSize(out, 256*1024)
		for {
			jpeg, err := readJPEG(reader, 25<<20)
			select {
			case frames <- frameResult{jpeg: jpeg, err: err}:
			case <-readCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	var lastSaved time.Time
	receivedFrame := false
	watchdog := time.NewTimer(frameStartupTimeout)
	defer watchdog.Stop()
	for {
		select {
		case <-ctx.Done():
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return receivedFrame, ctx.Err()
		case <-watchdog.C:
			_ = cmd.Process.Kill()
			waitErr := cmd.Wait()
			phase := "waiting for first frame"
			if receivedFrame {
				phase = "without a new frame"
			}
			return receivedFrame, fmt.Errorf("stream watchdog timed out %s after %s (ffmpeg=%v)", phase, time.Since(started).Round(time.Millisecond), waitErr)
		case result := <-frames:
			if result.err != nil {
				waitErr := cmd.Wait()
				return receivedFrame, fmt.Errorf("frame stream ended after %s: read=%v ffmpeg=%v", time.Since(started).Round(time.Millisecond), result.err, waitErr)
			}
			jpeg := result.jpeg
			if !receivedFrame {
				log.Printf("camera recorder %s: first frame decoded in %s (%d bytes)", b.ID, time.Since(started).Round(time.Millisecond), len(jpeg))
				receivedFrame = true
			}
			if !watchdog.Stop() {
				select {
				case <-watchdog.C:
				default:
				}
			}
			watchdog.Reset(frameStallTimeout)
			r.publish(b.ID, jpeg)
			r.recordFrame(b.ID, len(jpeg))
			now := time.Now()
			if lastSaved.IsZero() || now.Sub(lastSaved) >= time.Duration(interval)*time.Second {
				if err := r.save(b, jpeg, now); err != nil {
					log.Printf("camera recorder %s: save: %v", b.Name, err)
				} else {
					if lastSaved.IsZero() {
						log.Printf("camera recorder %s: first snapshot saved to %s", b.ID, r.Latest(b.EnvironmentID, b.ID))
					}
					lastSaved = now
				}
			}
		}
	}
}

func ffmpegPath() string {
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "NOT FOUND"
	}
	return path
}

func streamDescription(raw string) string {
	if raw == "" {
		return "none"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "invalid URL"
	}
	host := u.Hostname()
	if port := u.Port(); port != "" {
		host += ":" + port
	}
	return fmt.Sprintf("%s://%s%s", u.Scheme, host, u.EscapedPath())
}

func sanitizeFFmpegLine(line, rawURL string) string {
	if rawURL != "" {
		line = strings.ReplaceAll(line, rawURL, streamDescription(rawURL))
	}
	if u, err := url.Parse(rawURL); err == nil && u.User != nil {
		if password, ok := u.User.Password(); ok && password != "" {
			line = strings.ReplaceAll(line, password, "***")
		}
	}
	return line
}

func (r *Recorder) migrateLegacyArchive() {
	bindings, err := r.store.Bindings()
	if err != nil {
		log.Printf("camera recorder: inspect legacy archive: %v", err)
		return
	}
	for _, b := range bindings {
		if b.Kind != domain.KindCamera {
			continue
		}
		oldPath := filepath.Join(r.legacyRoot, b.ID)
		if _, err := os.Stat(oldPath); err != nil {
			continue
		}
		newPath := r.cameraDir(b.EnvironmentID, b.ID)
		if _, err := os.Stat(newPath); err == nil {
			log.Printf("camera recorder %s: legacy archive retained at %s because destination exists", b.ID, oldPath)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(newPath), 0750); err != nil {
			log.Printf("camera recorder %s: create archive directory: %v", b.ID, err)
			continue
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			log.Printf("camera recorder %s: migrate legacy archive: %v", b.ID, err)
			continue
		}
		log.Printf("camera recorder %s: migrated archive to %s", b.ID, newPath)
	}
	_ = os.Remove(r.legacyRoot)
}

func readJPEG(r *bufio.Reader, max int) ([]byte, error) {
	var prev byte
	for {
		c, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		if prev == 0xff && c == 0xd8 {
			break
		}
		prev = c
	}
	image := []byte{0xff, 0xd8}
	prev = 0
	for len(image) < max {
		c, err := r.ReadByte()
		if err != nil {
			return nil, err
		}
		image = append(image, c)
		if prev == 0xff && c == 0xd9 {
			return image, nil
		}
		prev = c
	}
	return nil, errors.New("camera frame exceeds 25 MB")
}

func (r *Recorder) save(b domain.Binding, image []byte, now time.Time) error {
	dir := r.cameraDir(b.EnvironmentID, b.ID)
	archiveDir := filepath.Join(dir, now.Format("2006/01/02"))
	if err := os.MkdirAll(archiveDir, 0750); err != nil {
		return err
	}
	archive := filepath.Join(archiveDir, now.Format("20060102T150405.000")+".jpg")
	if err := atomicWrite(archive, image); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "latest.jpg"), image)
}

func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0640); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type archiveFile struct {
	path string
	at   time.Time
	size int64
}

// Usage is the on-disk footprint of one camera's archive, measured against the
// binding's configured limits.
type Usage struct {
	BindingID   string          `json:"bindingId"`
	Name        string          `json:"name"`
	Bytes       int64           `json:"bytes"`
	Snapshots   int             `json:"snapshots"`
	Oldest      *time.Time      `json:"oldest,omitempty"`
	Newest      *time.Time      `json:"newest,omitempty"`
	LimitBytes  int64           `json:"limitBytes"`  // 0 = unlimited
	LimitDays   int             `json:"limitDays"`   // 0 = unlimited
	Policy      RetentionPolicy `json:"policy"`      // what happens past a limit
	OverStorage bool            `json:"overStorage"` // size limit exceeded
	OverAge     bool            `json:"overAge"`     // snapshots older than the age limit exist
}

// RetentionPolicy mirrors domain.RetentionPolicy for JSON responses.
type RetentionPolicy = domain.RetentionPolicy

// scanArchive lists every archived snapshot under root. The capture time comes
// from the filename, not the file's mtime: copying, restoring or rsyncing an
// archive rewrites mtimes, and retention decisions must follow when a frame was
// actually taken.
func scanArchive(root string) ([]archiveFile, error) {
	var files []archiveFile
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Base(path) == "latest.jpg" || filepath.Ext(path) != ".jpg" {
			return nil
		}
		stamp := filepath.Base(path[:len(path)-len(filepath.Ext(path))])
		at, err := time.ParseInLocation("20060102T150405.000", stamp, time.Local)
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files = append(files, archiveFile{path: path, at: at, size: info.Size()})
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].at.Before(files[j].at) })
	return files, nil
}

// Usage measures one camera archive against its configured limits without
// changing anything on disk.
func (r *Recorder) Usage(b domain.Binding) (Usage, error) {
	files, err := scanArchive(r.cameraDir(b.EnvironmentID, b.ID))
	if err != nil {
		return Usage{}, err
	}
	usage := Usage{
		BindingID:  b.ID,
		Name:       b.Name,
		Snapshots:  len(files),
		LimitBytes: int64(b.CameraStorageMB) << 20,
		LimitDays:  b.CameraRetentionDays,
		Policy:     policyOf(b),
	}
	for _, f := range files {
		usage.Bytes += f.size
	}
	if len(files) > 0 {
		oldest, newest := files[0].at, files[len(files)-1].at
		usage.Oldest, usage.Newest = &oldest, &newest
		if b.CameraRetentionDays > 0 {
			usage.OverAge = oldest.Before(ageCutoff(b, time.Now()))
		}
	}
	usage.OverStorage = usage.LimitBytes > 0 && usage.Bytes > usage.LimitBytes
	return usage, nil
}

// EnvironmentUsage measures every RTSP camera archive belonging to one
// environment.
func (r *Recorder) EnvironmentUsage(environmentID string) ([]Usage, error) {
	bindings, err := r.store.Bindings()
	if err != nil {
		return nil, err
	}
	out := []Usage{}
	for _, b := range bindings {
		if b.EnvironmentID != environmentID || b.Kind != domain.KindCamera || b.CameraType != domain.CameraRTSP {
			continue
		}
		usage, err := r.Usage(b)
		if err != nil {
			return nil, err
		}
		out = append(out, usage)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bytes > out[j].Bytes })
	return out, nil
}

func policyOf(b domain.Binding) domain.RetentionPolicy {
	if b.CameraRetentionPolicy == domain.RetentionDelete {
		return domain.RetentionDelete
	}
	return domain.RetentionAlert
}

func ageCutoff(b domain.Binding, now time.Time) time.Time {
	return now.Add(-time.Duration(b.CameraRetentionDays) * 24 * time.Hour)
}

// enforceRetention checks every camera archive against its limits. Under the
// default "alert" policy nothing is ever removed — an exceeded limit only opens
// an alert. Deletion happens solely for bindings that opted into it.
func (r *Recorder) enforceRetention() {
	bindings, err := r.store.Bindings()
	if err != nil {
		log.Printf("camera retention: list bindings: %v", err)
		return
	}
	for _, b := range bindings {
		if b.Kind != domain.KindCamera || b.CameraType != domain.CameraRTSP {
			continue
		}
		if policyOf(b) == domain.RetentionDelete {
			if err := r.prune(b, time.Now()); err != nil {
				log.Printf("camera retention %s: prune: %v", b.ID, err)
			}
		}
		usage, err := r.Usage(b)
		if err != nil {
			log.Printf("camera retention %s: measure: %v", b.ID, err)
			continue
		}
		r.reportUsage(b, usage)
	}
}

// reportUsage keeps one alert per limit open for as long as it is exceeded.
func (r *Recorder) reportUsage(b domain.Binding, usage Usage) {
	storageKey := fmt.Sprintf("%s:camera-storage:%s", b.EnvironmentID, b.ID)
	if usage.OverStorage {
		r.raise(b, storageKey, "camera-storage",
			fmt.Sprintf("%s archive is over its storage limit", b.Name),
			fmt.Sprintf("%s uses %s of its %s limit. Nothing has been deleted — raise the limit, enable automatic deletion, or free space manually.",
				b.Name, formatBytes(usage.Bytes), formatBytes(usage.LimitBytes)))
	} else {
		r.clear(storageKey)
	}

	ageKey := fmt.Sprintf("%s:camera-retention:%s", b.EnvironmentID, b.ID)
	if usage.OverAge && usage.Oldest != nil {
		r.raise(b, ageKey, "camera-retention",
			fmt.Sprintf("%s archive is older than its retention limit", b.Name),
			fmt.Sprintf("%s holds snapshots back to %s, beyond the %d-day limit. Nothing has been deleted — raise the limit, enable automatic deletion, or remove old footage manually.",
				b.Name, usage.Oldest.Format("2 Jan 2006"), usage.LimitDays))
	} else {
		r.clear(ageKey)
	}
}

func (r *Recorder) raise(b domain.Binding, key, kind, title, message string) {
	if err := r.store.OpenAlert(domain.Alert{
		Key: key, EnvironmentID: b.EnvironmentID, DeviceID: b.DeviceID,
		Severity: domain.AlertWarning, Kind: kind, Title: title, Message: message,
	}); err != nil {
		log.Printf("camera retention %s: open alert: %v", b.ID, err)
	}
}

func (r *Recorder) clear(key string) {
	if err := r.store.ResolveAlert(key); err != nil {
		log.Printf("camera retention: resolve alert %s: %v", key, err)
	}
}

// prune deletes archived snapshots past the binding's limits, oldest first. It
// runs only for bindings whose policy is "delete".
func (r *Recorder) prune(b domain.Binding, now time.Time) error {
	root := r.cameraDir(b.EnvironmentID, b.ID)
	files, err := scanArchive(root)
	if err != nil {
		return err
	}
	var total int64
	for _, f := range files {
		total += f.size
	}
	removed := 0
	remaining := files[:0]
	if b.CameraRetentionDays > 0 {
		cutoff := ageCutoff(b, now)
		for _, f := range files {
			if f.at.Before(cutoff) && os.Remove(f.path) == nil {
				total -= f.size
				removed++
				continue
			}
			remaining = append(remaining, f)
		}
		files = remaining
	}
	if limit := int64(b.CameraStorageMB) << 20; limit > 0 {
		// files is sorted oldest first, so the oldest footage goes first.
		for _, f := range files {
			if total <= limit {
				break
			}
			if os.Remove(f.path) == nil {
				total -= f.size
				removed++
			}
		}
	}
	if removed > 0 {
		log.Printf("camera retention %s: deleted %d snapshot(s), archive now %s", b.ID, removed, formatBytes(total))
		pruneEmptyDirs(root)
	}
	return nil
}

// pruneEmptyDirs removes the year/month/day folders left behind once their
// snapshots are gone. root itself is kept — it still holds latest.jpg.
func pruneEmptyDirs(root string) {
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err == nil && d.IsDir() && path != root {
			dirs = append(dirs, path)
		}
		return nil
	})
	// Deepest first, so emptying a day folder can also empty its month.
	sort.Slice(dirs, func(i, j int) bool { return len(dirs[i]) > len(dirs[j]) })
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err == nil && len(entries) == 0 {
			_ = os.Remove(dir)
		}
	}
}

func formatBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
}
