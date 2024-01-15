package tools

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
)

const (
	// Compilers.
	GO  = "GO"
	CC  = "CC"
	CXX = "CXX"

	// Formatters.
	CLANG_FORMAT = "CLANG_FORMAT"
)

var (
	// tools is a map of tool ENV vars to their default binary.
	// If the ENV var is not set, the default binary will be used.
	// If the default is NOT a path, it will be looked up in the PATH.
	tools = map[string]string{
		GO: mg.GoCmd(),
		// CC:           "zig cc",
		// CXX:          "zig c++",
		// CLANG_FORMAT: "clang-format",
	}
)

// getToolKey returns the ENV var or the default binary.
func getToolKey(key string) string {
	if cmd := os.Getenv(key); cmd != "" {
		return cmd
	}
	if val, ok := tools[key]; ok {
		return val
	}
	panic(fmt.Errorf("no default binary for %s", key))
}

// exe returns the full path to the binary.
func exe(cmd string) string {
	exe := getToolKey(cmd)
	if strings.HasPrefix(exe, "./") || strings.HasPrefix(exe, "../") || strings.HasPrefix(exe, "/") {
		if _, err := os.Stat(exe); os.IsNotExist(err) {
			panic(fmt.Sprintf("binary %s does not exist: %s", cmd, exe))
		}
	}

	parts := strings.Split(exe, " ")
	if len(parts) > 1 {
		exe = parts[0]
	}
	if _, err := exec.LookPath(exe); err != nil {
		panic(fmt.Sprintf("binary %s does not exist in PATH: %s", cmd, exe))
	}

	return strings.Join(parts, " ")
}

// RunCmd runs the command with the given args.
func RunCmd(cmd string, args ...string) error {
	parts := strings.Split(cmd, " ")
	if len(parts) > 1 {
		return sh.Run(parts[0], append(parts[1:], args...)...)
	}
	return sh.Run(cmd, args...)
}
