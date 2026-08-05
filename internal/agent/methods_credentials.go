package agent

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/jaa/update-downloads/internal/auth"
	"github.com/jaa/update-downloads/internal/config"
)

type CredentialOperations struct {
	InspectSoundCloud func(string) auth.CredentialStatus
	InspectDeemix     func(string) auth.CredentialStatus
	InspectSpotify    func(string) auth.CredentialStatus
	InspectNavidrome  func(string) auth.CredentialStatus
	SaveSoundCloud    func(string) error
	SaveDeemix        func(string) error
	SaveSpotify       func(auth.SpotifyCredentials) error
	SaveNavidrome     func(string) error
	ClearSoundCloud   func() error
	ClearDeemix       func() error
	ClearSpotify      func() error
	ClearNavidrome    func() error
	ClearFailure      func(string, auth.CredentialKind) error
}

func defaultCredentialOperations() *CredentialOperations {
	return &CredentialOperations{
		InspectSoundCloud: auth.InspectSoundCloudClientID,
		InspectDeemix:     auth.InspectDeemixARL,
		InspectSpotify:    auth.InspectSpotifyCredentials,
		InspectNavidrome:  auth.InspectNavidromePassword,
		SaveSoundCloud:    auth.SaveSoundCloudClientID,
		SaveDeemix:        auth.SaveDeemixARL,
		SaveSpotify:       auth.SaveSpotifyCredentials,
		SaveNavidrome:     auth.SaveNavidromePassword,
		ClearSoundCloud:   auth.RemoveSoundCloudClientID,
		ClearDeemix:       auth.RemoveDeemixARL,
		ClearSpotify:      auth.RemoveSpotifyCredentials,
		ClearNavidrome:    auth.RemoveNavidromePassword,
		ClearFailure:      auth.ClearCredentialFailure,
	}
}

type credentialStatusResult struct {
	Kind               auth.CredentialKind          `json:"kind"`
	Title              string                       `json:"title"`
	Health             auth.CredentialHealth        `json:"health"`
	StorageSource      auth.CredentialStorageSource `json:"storage_source"`
	Summary            string                       `json:"summary"`
	LastCheckedAt      time.Time                    `json:"last_checked_at,omitempty"`
	LastFailureKind    string                       `json:"last_failure_kind,omitempty"`
	LastFailureMessage string                       `json:"last_failure_message,omitempty"`
}

type credentialMutationParams struct {
	Kind         auth.CredentialKind `json:"kind"`
	Value        string              `json:"value"`
	ClientID     string              `json:"client_id"`
	ClientSecret string              `json:"client_secret"`
}

func (s *Server) listCredentials() (any, *RPCError) {
	stateDir, rpcErr := s.credentialStateDir()
	if rpcErr != nil {
		return nil, rpcErr
	}
	ops := s.credentialOperations()
	// An inspector may be unset when a caller supplies a partial
	// CredentialOperations; skip it rather than panicking on the whole list.
	statuses := []auth.CredentialStatus{}
	for _, inspect := range []func(string) auth.CredentialStatus{
		ops.InspectSoundCloud, ops.InspectDeemix, ops.InspectSpotify, ops.InspectNavidrome,
	} {
		if inspect == nil {
			continue
		}
		statuses = append(statuses, inspect(stateDir))
	}
	result := make([]credentialStatusResult, 0, len(statuses))
	for _, status := range statuses {
		result = append(result, credentialStatusResult{
			Kind: status.Kind, Title: status.Title, Health: status.Health,
			StorageSource: status.StorageSource, Summary: status.Summary,
			LastCheckedAt: status.LastCheckedAt, LastFailureKind: status.LastFailureKind,
			LastFailureMessage: status.LastFailureMessage,
		})
	}
	return map[string]any{"credentials": result}, nil
}

func (s *Server) saveCredential(params json.RawMessage) (any, *RPCError) {
	var request credentialMutationParams
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid credentials.save params", nil)
	}
	ops := s.credentialOperations()
	var err error
	switch request.Kind {
	case auth.CredentialKindSoundCloudClientID:
		if strings.TrimSpace(request.Value) == "" {
			return nil, credentialInputError(request.Kind)
		}
		err = ops.SaveSoundCloud(request.Value)
	case auth.CredentialKindDeemixARL:
		if strings.TrimSpace(request.Value) == "" {
			return nil, credentialInputError(request.Kind)
		}
		err = ops.SaveDeemix(request.Value)
	case auth.CredentialKindSpotifyApp:
		if strings.TrimSpace(request.ClientID) == "" || strings.TrimSpace(request.ClientSecret) == "" {
			return nil, credentialInputError(request.Kind)
		}
		err = ops.SaveSpotify(auth.SpotifyCredentials{ClientID: request.ClientID, ClientSecret: request.ClientSecret})
	case auth.CredentialKindNavidromePassword:
		if strings.TrimSpace(request.Value) == "" {
			return nil, credentialInputError(request.Kind)
		}
		err = ops.SaveNavidrome(request.Value)
	default:
		return nil, NewRPCError(CodeInvalidParams, "unsupported credential kind", map[string]any{"kind": request.Kind})
	}
	if err != nil {
		return nil, credentialOperationError(request.Kind)
	}
	if stateDir, stateErr := s.credentialStateDir(); stateErr == nil && ops.ClearFailure != nil {
		_ = ops.ClearFailure(stateDir, request.Kind)
	}
	return map[string]any{"saved": true, "kind": request.Kind}, nil
}

func (s *Server) clearCredential(params json.RawMessage) (any, *RPCError) {
	var request credentialMutationParams
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid credentials.clear params", nil)
	}
	ops := s.credentialOperations()
	var err error
	switch request.Kind {
	case auth.CredentialKindSoundCloudClientID:
		err = ops.ClearSoundCloud()
	case auth.CredentialKindDeemixARL:
		err = ops.ClearDeemix()
	case auth.CredentialKindSpotifyApp:
		err = ops.ClearSpotify()
	case auth.CredentialKindNavidromePassword:
		err = ops.ClearNavidrome()
	default:
		return nil, NewRPCError(CodeInvalidParams, "unsupported credential kind", map[string]any{"kind": request.Kind})
	}
	if err != nil {
		return nil, credentialOperationError(request.Kind)
	}
	if stateDir, stateErr := s.credentialStateDir(); stateErr == nil && ops.ClearFailure != nil {
		_ = ops.ClearFailure(stateDir, request.Kind)
	}
	return map[string]any{"cleared": true, "kind": request.Kind}, nil
}

func (s *Server) credentialOperations() *CredentialOperations {
	if s.CredentialOps != nil {
		return s.CredentialOps
	}
	return defaultCredentialOperations()
}

func (s *Server) credentialStateDir() (string, *RPCError) {
	cfg, err := config.Load(config.LoadOptions{ExplicitPath: s.ConfigPath, WorkingDir: s.WorkingDir})
	if err != nil {
		return "", configRPCError(err)
	}
	stateDir := strings.TrimSpace(cfg.Defaults.StateDir)
	if stateDir == "" {
		stateDir = config.DefaultStateDir()
	}
	return stateDir, nil
}

func credentialInputError(kind auth.CredentialKind) *RPCError {
	return NewRPCError(CodeInvalidParams, "required credential fields must not be empty", map[string]any{"kind": kind})
}

func credentialOperationError(kind auth.CredentialKind) *RPCError {
	// Never include command output or request values: Keychain errors can echo
	// command arguments containing the secret.
	return NewRPCError(CodeInternalError, "credential operation failed; check Keychain permission and try again", map[string]any{"kind": kind})
}
