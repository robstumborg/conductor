package agent

import (
	"fmt"
	"os/exec"
	"strings"
)

var lookPath = exec.LookPath
var commandOutput = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

func ValidateName(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("agent is required")
	}
	if strings.ContainsAny(value, " \t\n\r") {
		return fmt.Errorf("invalid agent %q: agent names cannot contain whitespace", value)
	}
	return nil
}

func ListAvailable(agentCommand string) ([]string, error) {
	if _, err := lookPath(agentCommand); err != nil {
		return nil, fmt.Errorf("agent command %q not found", agentCommand)
	}
	out, err := commandOutput(agentCommand, "agent", "list")
	if err != nil {
		return nil, fmt.Errorf("failed to list agents from %s: %w", agentCommand, err)
	}
	lines := strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n")
	agents := make([]string, 0, len(lines))
	seen := make(map[string]struct{}, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, "\t ")
		if line == "" || strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		name := line
		if idx := strings.Index(line, " ("); idx > 0 {
			name = line[:idx]
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		agents = append(agents, name)
	}
	return agents, nil
}

func ValidateAvailable(agentCommand, value string) error {
	if err := ValidateName(value); err != nil {
		return err
	}
	agents, err := ListAvailable(agentCommand)
	if err != nil {
		return err
	}
	for _, agent := range agents {
		if agent == value {
			return nil
		}
	}
	return fmt.Errorf("agent %q is not available in this %s project; run '%s agent list' to see available agents", value, agentCommand, agentCommand)
}

func Resolve(defaultAgent, itemAgent, override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	if strings.TrimSpace(itemAgent) != "" {
		return strings.TrimSpace(itemAgent)
	}
	return strings.TrimSpace(defaultAgent)
}
