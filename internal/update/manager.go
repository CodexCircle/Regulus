package update

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sync"
	"time"
)

// Mode decides what the hub does when a newer release exists.
type Mode string

const (
	// ModeManual only tells the administrator that a new version exists.
	ModeManual Mode = "manual"
	// ModeDownload also downloads and checks it, then waits to be told to
	// apply it. The mode that applies it by itself (inside a maintenance
	// window, with rollback) comes with the code that can apply an update.
	ModeDownload Mode = "download"
)

// ErrAutomaticNotAvailable is returned for the automatic mode, which needs
// the part of the update system that applies a release.
var ErrAutomaticNotAvailable = errors.New("the automatic mode is not available yet: the hub can check for and download updates, but cannot apply them")

// Errors returned by the manager.
var (
	// ErrInvalidSettings wraps the explanation of settings that cannot be saved.
	ErrInvalidSettings = errors.New("invalid update settings")

	ErrChecksOff       = errors.New("update checks are turned off (updates.check in the configuration)")
	ErrBusy            = errors.New("an update check or download is already running")
	ErrTooSoon         = errors.New("the release server was asked a moment ago; wait a little before asking again")
	ErrNothingToFetch  = errors.New("there is no newer release to download")
	ErrNotReleaseBuild = errors.New("this build is not a release, so it cannot tell whether a release is newer")
)

// Settings are the update choices an administrator makes in the dashboard.
type Settings struct {
	Mode    Mode    `json:"mode"`
	Channel Channel `json:"channel"`
}

func validMode(m Mode) bool { return m == ModeManual || m == ModeDownload }

// Validate reports whether the settings can be saved.
func (s Settings) Validate() error {
	switch {
	case validMode(s.Mode):
	case s.Mode == "automatic":
		return ErrAutomaticNotAvailable
	default:
		return fmt.Errorf("%w: mode must be %q or %q", ErrInvalidSettings, ModeManual, ModeDownload)
	}
	if !s.Channel.Valid() {
		return fmt.Errorf("%w: channel must be %q or %q", ErrInvalidSettings, Stable, Beta)
	}
	return nil
}

// Info is a release as shown to the administrator.
type Info struct {
	Version     string    `json:"version"`
	Tag         string    `json:"tag"`
	Prerelease  bool      `json:"prerelease"`
	URL         string    `json:"url"`
	Notes       string    `json:"notes,omitempty"`
	PublishedAt time.Time `json:"published_at"`
}

func infoOf(r Release) *Info {
	return &Info{Version: r.Version.String(), Tag: r.Tag, Prerelease: r.Version.Prerelease(), URL: r.URL, Notes: r.Notes, PublishedAt: r.PublishedAt}
}

// Status is what the hub knows about updates, as the API reports it.
type Status struct {
	// CurrentVersion is the version of this hub as it was built.
	CurrentVersion string `json:"current_version"`
	// ReleaseBuild is false for a development build, which cannot compare
	// itself with releases: no update is ever reported for it.
	ReleaseBuild bool `json:"release_build"`
	// ChecksEnabled is false when updates.check is off: the hub never contacts
	// the release server.
	ChecksEnabled bool    `json:"checks_enabled"`
	Mode          Mode    `json:"mode"`
	Channel       Channel `json:"channel"`
	// Platform is the build of the release this hub would download.
	Platform string `json:"platform"`

	CheckedAt  *time.Time `json:"checked_at"`
	CheckError string     `json:"check_error,omitempty"`
	Checking   bool       `json:"checking"`
	// Latest is the newest release on the channel at the last check.
	Latest          *Info `json:"latest"`
	UpdateAvailable bool  `json:"update_available"`

	// Staged is the downloaded and checked release waiting in the cache.
	Staged        *Staged `json:"staged"`
	Downloading   bool    `json:"downloading"`
	DownloadError string  `json:"download_error,omitempty"`
}

// Auditor records events; *auth.Service implements it.
type Auditor interface {
	Record(ctx context.Context, actor, event, target, detail string)
}

// Audit events recorded by the manager.
const (
	EventSettings       = "update_settings_changed"
	EventDownloaded     = "update_downloaded"
	EventDownloadFailed = "update_download_failed"
)

// SystemActor is the actor of what the hub does by itself.
const SystemActor = "system"

const (
	keyMode    = "update.mode"
	keyChannel = "update.channel"
	keyState   = "update.state"

	// DefaultCheckEvery is how often the hub asks the release server.
	DefaultCheckEvery = 6 * time.Hour
	firstCheckDelay   = time.Minute
	minCheckGap       = 30 * time.Second
	downloadTimeout   = 30 * time.Minute
	checkTimeout      = 30 * time.Second
)

// Options configure a Manager.
type Options struct {
	DB     *sql.DB
	Log    *slog.Logger
	Source Source
	Stager *Stager
	Audit  Auditor
	// Version is the version the binary was built with.
	Version      string
	GOOS, GOARCH string
	// Enabled is false when the hub must not contact the release server.
	Enabled    bool
	CheckEvery time.Duration
	// Now may be replaced in tests.
	Now func() time.Time
}

// persisted is the part of the state that survives a restart.
type persisted struct {
	CheckedAt  time.Time `json:"checked_at"`
	CheckError string    `json:"check_error,omitempty"`
	// Channel is the channel Latest was found on; a different channel makes it
	// stale.
	Channel Channel `json:"channel"`
	Latest  *Info   `json:"latest,omitempty"`
}

// Manager looks for new releases and keeps the newest one downloaded. It does
// not apply anything.
type Manager struct {
	opts    Options
	current Version
	release bool

	wake chan struct{}
	wg   sync.WaitGroup

	mu        sync.Mutex
	base      context.Context
	state     persisted
	loaded    bool
	checking  bool
	staging   bool
	stageErr  string
	lastCheck time.Time
}

// New returns a Manager. Call Run to start the periodic checks.
func New(opts Options) *Manager {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.CheckEvery <= 0 {
		opts.CheckEvery = DefaultCheckEvery
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	m := &Manager{opts: opts, wake: make(chan struct{}, 1), base: context.Background()}
	m.current, m.release = ParseBuildVersion(opts.Version)
	return m
}

// Run checks for updates now and then until ctx ends. It returns at once when
// checks are off.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	m.base = ctx
	m.mu.Unlock()
	if !m.opts.Enabled {
		return
	}
	timer := time.NewTimer(m.firstDelay(ctx))
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		case <-m.wake:
		}
		m.cycle(ctx)
		timer.Reset(m.opts.CheckEvery + time.Duration(rand.Int64N(int64(m.opts.CheckEvery/10)+1)))
	}
}

// firstDelay waits out what is left of the interval since the last check, so a
// hub that restarts often does not ask the release server each time.
func (m *Manager) firstDelay(ctx context.Context) time.Duration {
	m.load(ctx)
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.state.CheckedAt.IsZero() {
		if left := m.opts.CheckEvery - m.opts.Now().Sub(m.state.CheckedAt); left > firstCheckDelay {
			return left
		}
	}
	return firstCheckDelay
}

// cycle is one round of the background work: check, then download if the mode
// says so.
func (m *Manager) cycle(ctx context.Context) {
	if !m.release {
		return
	}
	st, err := m.check(ctx)
	if err != nil {
		if !errors.Is(err, ErrBusy) {
			m.opts.Log.Warn("checking for updates", "err", err)
		}
		return
	}
	if st.Mode != ModeDownload || !st.UpdateAvailable {
		return
	}
	if st.Staged != nil && st.Staged.Version == st.Latest.Version {
		return
	}
	if err := m.download(ctx, SystemActor); err != nil && !errors.Is(err, ErrBusy) {
		m.opts.Log.Warn("downloading the update", "err", err)
	}
}

// Close waits for a download in progress to end. Cancel the context given to
// Run first.
func (m *Manager) Close() { m.wg.Wait() }

// Settings returns the saved choices, with the defaults for what was never
// chosen: manual mode, and the beta channel for a build that is itself a
// pre-release.
func (m *Manager) Settings(ctx context.Context) (Settings, error) {
	s := Settings{Mode: ModeManual, Channel: Stable}
	if m.release && m.current.Prerelease() {
		s.Channel = Beta
	}
	if v, ok, err := m.get(ctx, keyMode); err != nil {
		return s, err
	} else if ok && validMode(Mode(v)) {
		s.Mode = Mode(v)
	}
	if v, ok, err := m.get(ctx, keyChannel); err != nil {
		return s, err
	} else if ok && Channel(v).Valid() {
		s.Channel = Channel(v)
	}
	return s, nil
}

// SetSettings saves the choices. Changing the channel makes the last answer
// stale, so a new check starts at once.
func (m *Manager) SetSettings(ctx context.Context, by string, s Settings) (Status, error) {
	if err := s.Validate(); err != nil {
		return Status{}, err
	}
	old, err := m.Settings(ctx)
	if err != nil {
		return Status{}, err
	}
	if err := m.put(ctx, keyMode, string(s.Mode)); err != nil {
		return Status{}, err
	}
	if err := m.put(ctx, keyChannel, string(s.Channel)); err != nil {
		return Status{}, err
	}
	if old != s {
		m.audit(ctx, by, EventSettings, "updates", fmt.Sprintf("mode %s, channel %s", s.Mode, s.Channel))
	}
	if old.Channel != s.Channel {
		select {
		case m.wake <- struct{}{}:
		default:
		}
	}
	return m.Status(ctx)
}

// Status reports the current state.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	set, err := m.Settings(ctx)
	if err != nil {
		return Status{}, err
	}
	m.load(ctx)

	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{
		CurrentVersion: m.opts.Version,
		ReleaseBuild:   m.release,
		ChecksEnabled:  m.opts.Enabled,
		Mode:           set.Mode,
		Channel:        set.Channel,
		Platform:       m.opts.GOOS + "/" + m.opts.GOARCH,
		CheckError:     m.state.CheckError,
		Checking:       m.checking,
		Downloading:    m.staging,
		DownloadError:  m.stageErr,
	}
	if !m.state.CheckedAt.IsZero() {
		t := m.state.CheckedAt
		st.CheckedAt = &t
	}
	// An answer found on another channel says nothing about this one.
	if m.state.Latest != nil && m.state.Channel == set.Channel {
		st.Latest = m.state.Latest
		st.UpdateAvailable = m.newer(*m.state.Latest)
	}
	if m.opts.Stager != nil {
		if staged, ok := m.opts.Stager.Current(); ok && (!m.release || m.newer(Info{Version: staged.Version})) {
			st.Staged = &staged
		}
	}
	return st, nil
}

// newer reports whether the release is newer than this build. A build that is
// not a release is never behind.
func (m *Manager) newer(r Info) bool {
	if !m.release {
		return false
	}
	v, err := ParseVersion(r.Version)
	return err == nil && v.Compare(m.current) > 0
}

// Check asks the release server now, for an administrator who pressed the
// button. It refuses to do so more than once in a short while.
func (m *Manager) Check(ctx context.Context) (Status, error) {
	if !m.opts.Enabled {
		return Status{}, ErrChecksOff
	}
	m.mu.Lock()
	if !m.lastCheck.IsZero() && m.opts.Now().Sub(m.lastCheck) < minCheckGap {
		m.mu.Unlock()
		return Status{}, ErrTooSoon
	}
	m.mu.Unlock()
	st, err := m.check(ctx)
	if err != nil {
		return Status{}, err
	}
	return st, nil
}

// errCheckFailed wraps a failure to get an answer from the release server, so
// the API can tell it from a failure of the hub.
type errCheckFailed struct{ err error }

func (e errCheckFailed) Error() string { return e.err.Error() }
func (e errCheckFailed) Unwrap() error { return e.err }

// IsUpstream reports whether err is a failure of the release server or of the
// way to it, not of the hub.
func IsUpstream(err error) bool {
	var e errCheckFailed
	return errors.As(err, &e)
}

func (m *Manager) check(ctx context.Context) (Status, error) {
	m.mu.Lock()
	if m.checking {
		m.mu.Unlock()
		return Status{}, ErrBusy
	}
	m.checking = true
	m.lastCheck = m.opts.Now()
	m.mu.Unlock()

	set, serr := m.Settings(ctx)
	var releases []Release
	var err error
	if serr == nil {
		cctx, cancel := context.WithTimeout(ctx, checkTimeout)
		releases, err = m.opts.Source.Releases(cctx)
		cancel()
	}

	m.mu.Lock()
	m.checking = false
	switch {
	case serr != nil:
		m.mu.Unlock()
		return Status{}, serr
	case err != nil:
		// Keep what the last good check found; only the error is new.
		m.state.CheckError = err.Error()
		m.state.CheckedAt = m.opts.Now().UTC()
	default:
		m.state = persisted{CheckedAt: m.opts.Now().UTC(), Channel: set.Channel}
		if best, ok := Latest(releases, set.Channel); ok {
			m.state.Latest = infoOf(best)
		}
	}
	snapshot := m.state
	m.mu.Unlock()

	if perr := m.saveState(ctx, snapshot); perr != nil {
		m.opts.Log.Warn("saving the update state", "err", perr)
	}
	if err != nil {
		return Status{}, errCheckFailed{err}
	}
	return m.Status(ctx)
}

// Download starts downloading the newest release on the channel in the
// background and returns at once; Status shows the progress. The download is
// checked against the release's checksums before it is kept.
func (m *Manager) Download(ctx context.Context, by string) (Status, error) {
	if !m.opts.Enabled {
		return Status{}, ErrChecksOff
	}
	if !m.release {
		return Status{}, ErrNotReleaseBuild
	}
	st, err := m.Status(ctx)
	if err != nil {
		return Status{}, err
	}
	if !st.UpdateAvailable {
		return Status{}, ErrNothingToFetch
	}

	m.mu.Lock()
	if m.staging {
		m.mu.Unlock()
		return Status{}, ErrBusy
	}
	m.staging = true
	m.stageErr = ""
	base := m.base
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.stage(base, by)
	}()
	return m.Status(ctx)
}

// download stages the latest release and waits for it; the background cycle
// uses it.
func (m *Manager) download(ctx context.Context, by string) error {
	m.mu.Lock()
	if m.staging {
		m.mu.Unlock()
		return ErrBusy
	}
	m.staging = true
	m.stageErr = ""
	m.mu.Unlock()
	return m.stage(ctx, by)
}

// stage downloads the release (the caller has set m.staging) and records the
// outcome. It clears m.staging.
func (m *Manager) stage(ctx context.Context, by string) error {
	err := m.doStage(ctx, by)
	m.mu.Lock()
	m.staging = false
	if err != nil {
		m.stageErr = err.Error()
	}
	m.mu.Unlock()
	return err
}

func (m *Manager) doStage(ctx context.Context, by string) error {
	ctx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	set, err := m.Settings(ctx)
	if err != nil {
		return err
	}
	// The list is asked again (a cheap conditional request) for the asset
	// addresses, which are not kept between checks.
	releases, err := m.opts.Source.Releases(ctx)
	if err != nil {
		return m.failed(ctx, by, "", err)
	}
	best, ok := Latest(releases, set.Channel)
	if !ok || best.Version.Compare(m.current) <= 0 {
		return ErrNothingToFetch
	}
	staged, err := m.opts.Stager.Stage(ctx, best, m.opts.GOOS, m.opts.GOARCH)
	if err != nil {
		return m.failed(ctx, by, best.Version.String(), err)
	}
	m.audit(ctx, by, EventDownloaded, staged.Version, fmt.Sprintf("%s, sha256 %s, verified by %s", staged.Archive, staged.SHA256, staged.Verified))
	m.opts.Log.Info("update downloaded", "version", staged.Version, "verified", staged.Verified)
	return nil
}

func (m *Manager) failed(ctx context.Context, by, version string, err error) error {
	m.audit(ctx, by, EventDownloadFailed, version, err.Error())
	return err
}

func (m *Manager) audit(ctx context.Context, actor, event, target, detail string) {
	if m.opts.Audit != nil {
		m.opts.Audit.Record(ctx, actor, event, target, detail)
	}
}

// State in the database (the settings table).

func (m *Manager) get(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := m.opts.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (m *Manager) put(ctx context.Context, key, value string) error {
	_, err := m.opts.DB.ExecContext(ctx,
		`INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// load reads the saved state once.
func (m *Manager) load(ctx context.Context) {
	m.mu.Lock()
	done := m.loaded
	m.mu.Unlock()
	if done {
		return
	}
	var p persisted
	if v, ok, err := m.get(ctx, keyState); err == nil && ok {
		_ = json.Unmarshal([]byte(v), &p) // a damaged value only means "never checked"
	}
	m.mu.Lock()
	if !m.loaded {
		m.state, m.loaded = p, true
	}
	m.mu.Unlock()
}

func (m *Manager) saveState(ctx context.Context, p persisted) error {
	data, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return m.put(ctx, keyState, string(data))
}
