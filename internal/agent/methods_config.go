package agent

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jaa/update-downloads/internal/config"
)

type configFileParams struct {
	Path                  string        `json:"path"`
	Config                config.Config `json:"config"`
	ExpectedContentSHA256 string        `json:"expected_content_sha256,omitempty"`
}

type configFileResult struct {
	Path          string        `json:"path"`
	Config        config.Config `json:"config"`
	Content       string        `json:"content"`
	ContentSHA256 string        `json:"content_sha256"`
}

func (s *Server) loadConfig() (any, *RPCError) {
	cfg, err := config.Load(config.LoadOptions{ExplicitPath: s.ConfigPath, WorkingDir: s.WorkingDir})
	if err != nil {
		return nil, configRPCError(err)
	}
	return map[string]any{"config": cfg}, nil
}

func (s *Server) validateConfig(params json.RawMessage) (any, *RPCError) {
	var request struct {
		Config config.Config `json:"config"`
	}
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid config.validate params", nil)
	}
	if err := config.Validate(request.Config); err != nil {
		return nil, configRPCError(err)
	}
	return map[string]bool{"valid": true}, nil
}

func (s *Server) readConfigFile(params json.RawMessage) (any, *RPCError) {
	var request configFileParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &request); err != nil {
			return nil, NewRPCError(CodeInvalidParams, "invalid config.readFile params", nil)
		}
	}
	path, rpcErr := s.resolveScopedConfigPath(request.Path)
	if rpcErr != nil {
		return nil, rpcErr
	}
	cfg, err := config.LoadSingleFile(path)
	if err != nil {
		return nil, configRPCError(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, configRPCError(err)
	}
	return configFileResult{
		Path: path, Config: cfg, Content: string(content), ContentSHA256: contentSHA256(content),
	}, nil
}

func (s *Server) writeConfigFile(params json.RawMessage) (any, *RPCError) {
	var request configFileParams
	if err := json.Unmarshal(params, &request); err != nil {
		return nil, NewRPCError(CodeInvalidParams, "invalid config.writeFile params", nil)
	}
	path, rpcErr := s.resolveScopedConfigPath(request.Path)
	if rpcErr != nil {
		return nil, rpcErr
	}
	if err := config.Validate(request.Config); err != nil {
		return nil, configRPCError(err)
	}
	if expected := strings.TrimSpace(request.ExpectedContentSHA256); expected != "" {
		current, err := os.ReadFile(path)
		if err != nil {
			return nil, configRPCError(err)
		}
		actual := contentSHA256(current)
		if actual != expected {
			return nil, NewRPCError(CodeRunConflict, "config changed outside UDL; reload before saving", map[string]string{
				"path": path, "expected_content_sha256": expected, "actual_content_sha256": actual,
			})
		}
	}
	canonical, err := config.MarshalCanonical(request.Config)
	if err != nil {
		return nil, configRPCError(err)
	}
	if _, err := config.SaveSingleFile(path, request.Config); err != nil {
		return nil, configRPCError(err)
	}
	return configFileResult{
		Path: path, Config: request.Config, Content: string(canonical), ContentSHA256: contentSHA256(canonical),
	}, nil
}

func contentSHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return fmt.Sprintf("%x", sum[:])
}

func (s *Server) resolveScopedConfigPath(requested string) (string, *RPCError) {
	allowed, err := s.allowedConfigPaths()
	if err != nil {
		return "", NewRPCError(CodeInternalError, err.Error(), nil)
	}
	candidate := strings.TrimSpace(requested)
	if candidate == "" {
		if strings.TrimSpace(s.ConfigPath) != "" {
			candidate = s.ConfigPath
		} else {
			project := filepath.Join(s.WorkingDir, "udl.yaml")
			if _, statErr := os.Stat(project); statErr == nil {
				candidate = project
			} else {
				user, userErr := config.UserConfigPath()
				if userErr != nil {
					return "", NewRPCError(CodeInternalError, userErr.Error(), nil)
				}
				candidate = user
			}
		}
	}
	expanded, err := config.ExpandPath(candidate)
	if err != nil {
		return "", NewRPCError(CodeInvalidParams, "invalid config path", nil)
	}
	absolute, err := filepath.Abs(expanded)
	if err != nil {
		return "", NewRPCError(CodeInvalidParams, "invalid config path", nil)
	}
	absolute = filepath.Clean(absolute)
	if !allowed[absolute] {
		return "", NewRPCError(CodeInvalidParams, "config path is outside the initialized session scope", map[string]string{"path": absolute})
	}
	if info, statErr := os.Lstat(absolute); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", NewRPCError(CodeInvalidParams, "symbolic-link config paths are not allowed", map[string]string{"path": absolute})
	}
	return absolute, nil
}

func (s *Server) allowedConfigPaths() (map[string]bool, error) {
	allowed := map[string]bool{}
	add := func(path string) error {
		expanded, err := config.ExpandPath(path)
		if err != nil {
			return err
		}
		absolute, err := filepath.Abs(expanded)
		if err != nil {
			return err
		}
		allowed[filepath.Clean(absolute)] = true
		return nil
	}
	if strings.TrimSpace(s.ConfigPath) != "" {
		if err := add(s.ConfigPath); err != nil {
			return nil, err
		}
		return allowed, nil
	}
	if err := add(filepath.Join(s.WorkingDir, "udl.yaml")); err != nil {
		return nil, err
	}
	user, err := config.UserConfigPath()
	if err != nil {
		return nil, err
	}
	if err := add(user); err != nil {
		return nil, err
	}
	return allowed, nil
}

func configRPCError(err error) *RPCError {
	var validation *config.ValidationError
	if errors.As(err, &validation) {
		return NewRPCError(CodeInvalidParams, "invalid config", map[string]any{"problems": validation.Problems})
	}
	return NewRPCError(CodeInvalidParams, err.Error(), nil)
}
