package runner

import (
	"fmt"
	"os"
	"strings"
)

func expandLaunchArgs(args []string, vars map[string]string) []string {
	expanded := make([]string, 0, len(args))
	for _, item := range args {
		out := item
		for key, val := range vars {
			out = strings.ReplaceAll(out, "{"+key+"}", val)
		}
		expanded = append(expanded, out)
	}
	return expanded
}

func validateLaunch(command string) error {
	if strings.TrimSpace(command) == "" {
		return fmt.Errorf("runner launch_command is required for per-task mode")
	}
	return nil
}

func defaultLaunchCommand() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return path, nil
}
