package navidrome

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ServiceState is the launchd state of the managed agent.
type ServiceState string

const (
	// ServiceNotInstalled means no UDL-owned LaunchAgent exists.
	ServiceNotInstalled ServiceState = "not_installed"
	// ServiceStopped means the agent exists but is not loaded or not running.
	ServiceStopped ServiceState = "stopped"
	// ServiceRunning means launchd reports a live PID.
	ServiceRunning ServiceState = "running"
	// ServiceUnknown means launchd could not be queried.
	ServiceUnknown ServiceState = "unknown"
)

// ServiceStatus is a read-only view of the managed service.
type ServiceStatus struct {
	State           ServiceState `json:"state"`
	Loaded          bool         `json:"loaded"`
	PID             int          `json:"pid,omitempty"`
	LastExitStatus  int          `json:"last_exit_status,omitempty"`
	LaunchAgentPath string       `json:"launch_agent_path,omitempty"`
	Owned           bool         `json:"owned"`
	LocalURL        string       `json:"local_url,omitempty"`
	LANURL          string       `json:"lan_url,omitempty"`
	Hostname        string       `json:"hostname,omitempty"`
	LogPath         string       `json:"log_path,omitempty"`
	Problems        []string     `json:"problems"`
}

// Service performs launchd lifecycle operations on the UDL-owned agent.
type Service struct {
	Run CommandRunner
	// UID overrides the launchd gui domain target in tests.
	UID string
	// LookupHost resolves the LAN address shown to the user.
	LookupHost func() (string, string)
	// Wait paces the bootstrap retries. Tests substitute a no-op.
	Wait func(time.Duration)
}

// bootstrapAttempts covers the window after a bootout in which launchd still
// holds the old job and answers a fresh bootstrap with "5: Input/output error".
// Observed on a real restart: the immediate retry succeeds.
const bootstrapAttempts = 5

func (s Service) wait(d time.Duration) {
	if s.Wait != nil {
		s.Wait(d)
		return
	}
	time.Sleep(d)
}

func (s Service) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if s.Run != nil {
		return s.Run(ctx, name, args...)
	}
	return DefaultCommandRunner(ctx, name, args...)
}

func (s Service) domainTarget() string {
	uid := strings.TrimSpace(s.UID)
	if uid == "" {
		uid = strconv.Itoa(os.Getuid())
	}
	return "gui/" + uid
}

func (s Service) serviceTarget() string {
	return s.domainTarget() + "/" + LaunchAgentLabel
}

// Status reports the launchd state without changing it.
func (s Service) Status(ctx context.Context, cfg Config, resolved Resolved) ServiceStatus {
	status := ServiceStatus{
		State:           ServiceNotInstalled,
		LaunchAgentPath: resolved.LaunchAgent,
		LogPath:         resolved.LogFile,
		Problems:        []string{},
	}
	payload, err := os.ReadFile(resolved.LaunchAgent)
	if err != nil {
		if !os.IsNotExist(err) {
			status.State = ServiceUnknown
			status.Problems = append(status.Problems, fmt.Sprintf("read LaunchAgent %s: %v", resolved.LaunchAgent, err))
		} else {
			status.Problems = append(status.Problems, "the UDL-managed Navidrome service is not installed yet")
		}
		s.fillURLs(&status, cfg)
		return status
	}
	status.Owned = IsUDLOwned(string(payload))
	if !status.Owned {
		status.State = ServiceUnknown
		status.Problems = append(status.Problems,
			fmt.Sprintf("%s exists but is not UDL-managed; UDL will not modify it", resolved.LaunchAgent))
		s.fillURLs(&status, cfg)
		return status
	}

	raw, err := s.run(ctx, "launchctl", "print", s.serviceTarget())
	if err != nil {
		status.State = ServiceStopped
		status.Problems = append(status.Problems, "the service is installed but not loaded in launchd")
		s.fillURLs(&status, cfg)
		return status
	}
	status.Loaded = true
	text := string(raw)
	status.PID = parseLaunchctlInt(text, "pid")
	status.LastExitStatus = parseLaunchctlInt(text, "last exit code")
	if status.PID > 0 {
		status.State = ServiceRunning
	} else {
		status.State = ServiceStopped
		status.Problems = append(status.Problems, "the service is loaded but no process is running; check the log")
	}
	s.fillURLs(&status, cfg)
	return status
}

func (s Service) fillURLs(status *ServiceStatus, cfg Config) {
	status.LocalURL = fmt.Sprintf("http://localhost:%d", cfg.Server.Port)
	hostname, ip := s.hosts()
	status.Hostname = hostname
	switch {
	case hostname != "":
		status.LANURL = fmt.Sprintf("http://%s:%d", hostname, cfg.Server.Port)
	case ip != "":
		status.LANURL = fmt.Sprintf("http://%s:%d", ip, cfg.Server.Port)
	}
}

func (s Service) hosts() (string, string) {
	if s.LookupHost != nil {
		return s.LookupHost()
	}
	return LocalHostNames()
}

// LocalHostNames returns the mDNS hostname and the primary LAN IPv4 address.
//
// `os.Hostname` can return a DHCP-assigned fully qualified name from the ISP
// (for example `MacBookPro.webspeed.dk`), which does not resolve from a phone
// on the same Wi-Fi. The Bonjour name from `scutil --get LocalHostName` is the
// one that does, so it is preferred and only the first label of the system
// hostname is used as a fallback.
func LocalHostNames() (string, string) {
	hostname := ""
	if raw, err := exec.Command("scutil", "--get", "LocalHostName").Output(); err == nil {
		if name := strings.TrimSpace(string(raw)); name != "" {
			hostname = name + ".local"
		}
	}
	if hostname == "" {
		if name, err := os.Hostname(); err == nil {
			short, _, _ := strings.Cut(strings.TrimSpace(name), ".")
			if short != "" {
				hostname = short + ".local"
			}
		}
	}
	ip := ""
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.IsLoopback() || ipNet.IP.To4() == nil {
				continue
			}
			ip = ipNet.IP.String()
			break
		}
	}
	return hostname, ip
}

// Start bootstraps and kickstarts the agent.
func (s Service) Start(ctx context.Context, resolved Resolved) error {
	if err := s.requireOwned(resolved); err != nil {
		return err
	}
	for attempt := 1; ; attempt++ {
		raw, err := s.run(ctx, "launchctl", "bootstrap", s.domainTarget(), resolved.LaunchAgent)
		if err == nil {
			break
		}
		lower := strings.ToLower(string(raw))
		// Already bootstrapped is the common, harmless case; kickstart below
		// still brings the process up.
		if strings.Contains(lower, "already") {
			break
		}
		// launchd needs a moment to release a job that was just booted out.
		if strings.Contains(lower, "input/output error") && attempt < bootstrapAttempts {
			s.wait(time.Duration(attempt) * 200 * time.Millisecond)
			continue
		}
		return fmt.Errorf("launchctl bootstrap: %w: %s", err, strings.TrimSpace(string(raw)))
	}
	if raw, err := s.run(ctx, "launchctl", "kickstart", "-k", s.serviceTarget()); err != nil {
		return fmt.Errorf("launchctl kickstart: %w: %s", err, strings.TrimSpace(string(raw)))
	}
	return nil
}

// Stop removes the agent from launchd.
func (s Service) Stop(ctx context.Context, resolved Resolved) error {
	if err := s.requireOwned(resolved); err != nil {
		return err
	}
	if raw, err := s.run(ctx, "launchctl", "bootout", s.serviceTarget()); err != nil {
		lower := strings.ToLower(string(raw))
		if strings.Contains(lower, "no such process") || strings.Contains(lower, "could not find") {
			return nil
		}
		return fmt.Errorf("launchctl bootout: %w: %s", err, strings.TrimSpace(string(raw)))
	}
	return nil
}

// Restart stops and starts the agent.
func (s Service) Restart(ctx context.Context, resolved Resolved) error {
	if err := s.Stop(ctx, resolved); err != nil {
		return err
	}
	return s.Start(ctx, resolved)
}

func (s Service) requireOwned(resolved Resolved) error {
	payload, err := os.ReadFile(resolved.LaunchAgent)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("the UDL-managed Navidrome service is not installed; run setup apply first")
		}
		return fmt.Errorf("read LaunchAgent %s: %w", resolved.LaunchAgent, err)
	}
	if !IsUDLOwned(string(payload)) {
		return fmt.Errorf("%s is not UDL-managed; UDL will not modify it", resolved.LaunchAgent)
	}
	return nil
}

// PortListener reports the PID listening on the port, or 0 when free. An
// unavailable lsof yields ok=false rather than a false accusation.
func (s Service) PortListener(ctx context.Context, port int) (int, bool) {
	raw, err := s.run(ctx, "lsof", "-nP", fmt.Sprintf("-iTCP:%d", port), "-sTCP:LISTEN", "-t")
	if err != nil {
		// lsof exits 1 with no output when nothing matches.
		if strings.TrimSpace(string(raw)) == "" {
			return 0, true
		}
		return 0, false
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 {
		return 0, true
	}
	pid, convErr := strconv.Atoi(fields[0])
	if convErr != nil {
		return 0, false
	}
	return pid, true
}

func parseLaunchctlInt(text, key string) int {
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		name, value, ok := strings.Cut(trimmed, "=")
		if !ok || strings.TrimSpace(name) != key {
			continue
		}
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return 0
		}
		return parsed
	}
	return 0
}
