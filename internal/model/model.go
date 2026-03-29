package model

import (
	"fmt"
	"os/exec"
	"strings"
)

var lookPath = exec.LookPath
var commandOutput = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).Output()
}

func ValidateFormat(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("model is required")
	}
	parts := strings.Split(value, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return fmt.Errorf("invalid model %q: expected provider/model", value)
	}
	return nil
}

func ListAvailable(agentCommand string) ([]string, error) {
	if _, err := lookPath(agentCommand); err != nil {
		return nil, fmt.Errorf("agent command %q not found", agentCommand)
	}
	out, err := commandOutput(agentCommand, "models")
	if err != nil {
		return nil, fmt.Errorf("failed to list models from %s: %w", agentCommand, err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	models := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			models = append(models, line)
		}
	}
	return models, nil
}

func ValidateAvailable(agentCommand, value string) error {
	if err := ValidateFormat(value); err != nil {
		return err
	}
	models, err := ListAvailable(agentCommand)
	if err != nil {
		return err
	}
	for _, model := range models {
		if model == value {
			return nil
		}
	}
	return fmt.Errorf("model %q is not available in this %s installation; run '%s models' to see available models", value, agentCommand, agentCommand)
}

func Resolve(defaultModel, itemModel, override string) string {
	if strings.TrimSpace(override) != "" {
		return strings.TrimSpace(override)
	}
	if strings.TrimSpace(itemModel) != "" {
		return strings.TrimSpace(itemModel)
	}
	return strings.TrimSpace(defaultModel)
}
