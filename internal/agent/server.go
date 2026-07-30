package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/jaa/update-downloads/internal/app"
	"github.com/jaa/update-downloads/internal/config"
	"github.com/jaa/update-downloads/internal/doctor"
	"github.com/jaa/update-downloads/internal/engine"
	"github.com/jaa/update-downloads/internal/freedl"
	"github.com/jaa/update-downloads/internal/playlists"
	"github.com/jaa/update-downloads/internal/rekordbox/syncconfig"
)

type BuildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

type Server struct {
	Conn                *Conn
	Runs                *RunRegistry
	Build               BuildInfo
	WorkingDir          string
	ConfigPath          string
	FreeDLConfigPath    string
	PlaylistsConfigPath string
	RekordboxConfigPath string
	ErrOut              io.Writer
	SyncRegistry        map[string]engine.Adapter
	SyncRunner          engine.ExecRunner
	DoctorChecker       *doctor.Checker
	CredentialOps       *CredentialOperations
	StartupInspectors   *app.CredentialInspectors
	PlaylistService     *playlists.Service
	FreeDLService       *freedl.Service
	FreeDLOps           *FreeDLOperations
	RekordboxOps        *RekordboxOperations

	mu           sync.Mutex
	initialized  bool
	shuttingDown bool
	cancel       context.CancelFunc
	runContext   context.Context
	ExtraMethods map[string]Handler
}

type initializeParams struct {
	ProtocolVersion int `json:"protocol_version"`
}

type initializeResult struct {
	ProtocolVersion    int                 `json:"protocol_version"`
	Build              BuildInfo           `json:"build"`
	Methods            []string            `json:"methods"`
	WorkingDir         string              `json:"working_dir"`
	ConfigPaths        []string            `json:"config_paths"`
	FeatureConfigPaths map[string][]string `json:"feature_config_paths"`
	Capabilities       map[string]any      `json:"capabilities"`
}

var protocolMethods = []string{
	"session.initialize",
	"session.shutdown",
	"run.cancel",
	"sync.start",
	"sync.cancel",
	"config.load",
	"config.validate",
	"config.readFile",
	"config.writeFile",
	"doctor.run",
	"credentials.list",
	"credentials.save",
	"credentials.clear",
	"startup.onboardingState",
	"startup.attention",
	"sources.capabilities",
	"playlists.list",
	"playlists.providerList",
	"playlists.show",
	"playlists.refresh",
	"playlists.saveDefinition",
	"playlists.config.read",
	"playlists.config.write",
	"freedl.config.read",
	"freedl.config.write",
	"freedl.plan.start",
	"freedl.capture.start",
	"freedl.promotionPlan.build",
	"freedl.promote.apply",
	"rekordbox.config.read",
	"rekordbox.config.write",
	"rekordbox.deps.status",
	"rekordbox.deps.ensure",
	"rekordbox.deps.reset",
	"rekordbox.inspect",
	"rekordbox.plan",
	"rekordbox.apply",
}

func (s *Server) Serve(ctx context.Context) error {
	if s.Conn == nil {
		return errors.New("agent server requires a connection")
	}
	if s.Runs == nil {
		s.Runs = NewRunRegistry()
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.cancel = cancel
	s.runContext = runCtx
	s.mu.Unlock()
	defer func() {
		cancel()
		s.Runs.CloseAll()
	}()
	err := s.Conn.Serve(runCtx, s.handle)
	s.mu.Lock()
	graceful := s.shuttingDown
	s.mu.Unlock()
	if graceful && (errors.Is(err, context.Canceled) || errors.Is(err, io.EOF)) {
		return nil
	}
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (s *Server) handle(method string, params json.RawMessage) (any, *RPCError) {
	if method == "session.initialize" {
		return s.initialize(params)
	}
	s.mu.Lock()
	initialized := s.initialized
	s.mu.Unlock()
	if !initialized {
		return nil, NewRPCError(CodeNotInitialized, "session.initialize must be called first", nil)
	}
	switch method {
	case "session.shutdown":
		return AfterResponse{Result: map[string]bool{"shutdown": true}, After: s.beginShutdown}, nil
	case "run.cancel":
		return s.cancelRun(params)
	case "sync.cancel":
		return s.cancelRun(params)
	case "sync.start":
		return s.startSync(params)
	case "config.load":
		return s.loadConfig()
	case "config.validate":
		return s.validateConfig(params)
	case "config.readFile":
		return s.readConfigFile(params)
	case "config.writeFile":
		return s.writeConfigFile(params)
	case "doctor.run":
		return s.runDoctor()
	case "credentials.list":
		return s.listCredentials()
	case "credentials.save":
		return s.saveCredential(params)
	case "credentials.clear":
		return s.clearCredential(params)
	case "startup.onboardingState":
		return s.onboardingState()
	case "startup.attention":
		return s.startupAttention()
	case "sources.capabilities":
		return s.sourceCapabilities()
	case "playlists.list":
		return s.listPlaylists()
	case "playlists.providerList":
		return s.listProviderPlaylists(params)
	case "playlists.show":
		return s.showPlaylist(params)
	case "playlists.refresh":
		return s.refreshPlaylist(params)
	case "playlists.saveDefinition":
		return s.savePlaylistDefinition(params)
	case "playlists.config.read":
		return s.readPlaylistsConfig()
	case "playlists.config.write":
		return s.writePlaylistsConfig(params)
	case "freedl.config.read":
		return s.readFreeDLConfig()
	case "freedl.config.write":
		return s.writeFreeDLConfig(params)
	case "freedl.plan.start":
		return s.startFreeDLPlan(params)
	case "freedl.capture.start":
		return s.startFreeDLCapture(params)
	case "freedl.promotionPlan.build":
		return s.buildFreeDLPromotionPlan(params)
	case "freedl.promote.apply":
		return s.applyFreeDLPromotion(params)
	case "rekordbox.config.read":
		return s.readRekordboxConfig()
	case "rekordbox.config.write":
		return s.writeRekordboxConfig(params)
	case "rekordbox.deps.status":
		return s.rekordboxDepsStatus()
	case "rekordbox.deps.ensure":
		return s.ensureRekordboxDeps()
	case "rekordbox.deps.reset":
		return s.resetRekordboxDeps()
	case "rekordbox.inspect":
		return s.inspectRekordbox()
	case "rekordbox.plan":
		return s.planRekordbox(params)
	case "rekordbox.apply":
		return s.applyRekordbox(params)
	default:
		if handler := s.ExtraMethods[method]; handler != nil {
			return handler(method, params)
		}
		return nil, NewRPCError(CodeMethodNotFound, "method not found", map[string]string{"method": method})
	}
}

func (s *Server) cancelRun(params json.RawMessage) (any, *RPCError) {
	var request struct {
		RunID string `json:"run_id"`
	}
	if err := json.Unmarshal(params, &request); err != nil || strings.TrimSpace(request.RunID) == "" {
		return nil, NewRPCError(CodeInvalidParams, "run_id must be set", nil)
	}
	if !s.Runs.Cancel(request.RunID) {
		return nil, NewRPCError(CodeRunNotFound, "run not found", map[string]string{"run_id": request.RunID})
	}
	return map[string]bool{"canceled": true}, nil
}

type RunWork func(context.Context, string) (result any, err error, exitCode int)

type runFinishedParams struct {
	RunID    string `json:"run_id"`
	Result   any    `json:"result,omitempty"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exit_code"`
}

func (s *Server) StartRun(work RunWork) (string, error) {
	if work == nil {
		return "", NewRPCError(CodeInvalidParams, "run work must be set", nil)
	}
	s.mu.Lock()
	parent := s.runContext
	s.mu.Unlock()
	if parent == nil {
		parent = context.Background()
	}
	runID, runCtx, err := s.Runs.Register(parent)
	if err != nil {
		return "", err
	}
	go func() {
		result, runErr, exitCode := work(runCtx, runID)
		if !s.Runs.Finish(runID) {
			return
		}
		finished := runFinishedParams{RunID: runID, Result: result, ExitCode: exitCode}
		if runErr != nil {
			finished.Error = runErr.Error()
		}
		_ = s.Conn.Notify("run.finished", finished)
	}()
	return runID, nil
}

func (s *Server) initialize(params json.RawMessage) (any, *RPCError) {
	var request initializeParams
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid session.initialize params", nil)
	}
	if request.ProtocolVersion != ProtocolVersion {
		return nil, NewRPCError(CodeInvalidParams, "unsupported protocol version", map[string]int{
			"requested": request.ProtocolVersion, "supported": ProtocolVersion,
		})
	}
	s.mu.Lock()
	s.initialized = true
	s.mu.Unlock()
	allowedPaths, err := s.allowedConfigPaths()
	if err != nil {
		return nil, NewRPCError(CodeInternalError, "resolve config discovery paths", nil)
	}
	configPaths := make([]string, 0, len(allowedPaths))
	for path := range allowedPaths {
		configPaths = append(configPaths, path)
	}
	sort.Strings(configPaths)
	featureConfigPaths, err := s.featureConfigDiscoveryPaths()
	if err != nil {
		return nil, NewRPCError(CodeInternalError, "resolve feature config discovery paths", nil)
	}
	return initializeResult{
		ProtocolVersion:    ProtocolVersion,
		Build:              s.Build,
		Methods:            append([]string(nil), protocolMethods...),
		WorkingDir:         filepath.Clean(s.WorkingDir),
		ConfigPaths:        configPaths,
		FeatureConfigPaths: featureConfigPaths,
		Capabilities: map[string]any{
			"bidirectional_ui": true,
			"concurrent_runs":  true,
			"max_frame_bytes":  MaxFrameBytes,
		},
	}, nil
}

func (s *Server) featureConfigDiscoveryPaths() (map[string][]string, error) {
	result := map[string][]string{}
	resolve := func(paths ...string) ([]string, error) {
		seen := map[string]bool{}
		resolved := []string{}
		for _, path := range paths {
			if strings.TrimSpace(path) == "" {
				continue
			}
			expanded, err := config.ExpandPath(path)
			if err != nil {
				return nil, err
			}
			absolute, err := filepath.Abs(expanded)
			if err != nil {
				return nil, err
			}
			absolute = filepath.Clean(absolute)
			if !seen[absolute] {
				seen[absolute] = true
				resolved = append(resolved, absolute)
			}
		}
		sort.Strings(resolved)
		return resolved, nil
	}
	var err error
	if strings.TrimSpace(s.FreeDLConfigPath) != "" {
		result["freedl"], err = resolve(s.FreeDLConfigPath)
	} else {
		user, userErr := freedl.UserConfigPath()
		if userErr != nil {
			return nil, userErr
		}
		result["freedl"], err = resolve(user, freedl.ProjectConfigPath(s.WorkingDir))
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(s.PlaylistsConfigPath) != "" {
		result["playlists"], err = resolve(s.PlaylistsConfigPath)
	} else {
		user, userErr := playlists.UserConfigPath()
		if userErr != nil {
			return nil, userErr
		}
		result["playlists"], err = resolve(user, playlists.ProjectConfigPath(s.WorkingDir))
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(s.RekordboxConfigPath) != "" {
		result["rekordbox"], err = resolve(s.RekordboxConfigPath)
	} else {
		user, userErr := syncconfig.UserConfigPath()
		if userErr != nil {
			return nil, userErr
		}
		result["rekordbox"], err = resolve(user, syncconfig.ProjectConfigPath(s.WorkingDir))
	}
	return result, err
}

func (s *Server) beginShutdown() {
	s.mu.Lock()
	if s.shuttingDown {
		s.mu.Unlock()
		return
	}
	s.shuttingDown = true
	cancel := s.cancel
	s.mu.Unlock()
	s.Runs.CloseAll()
	if cancel != nil {
		cancel()
	}
}
