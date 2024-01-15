package tools

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/magefile/mage/sh"
)

const (
	// Intsallation directory.
	toolsDir = "_tools"

	// Helper Go tools.
	JSONNET    = "JSONNET"
	JSONNETFMT = "JSONNETFMT"
	JB         = "JB"
	JSONTOYAML = "JSONTOYAML"
)

const (
	// Tool versions.
	// TODO(kakkoyun): Make sure renovate directives work as expected.

	// renovate: datasource=go depName=github.com/google/go-jsonnet
	JSONNET_VERSION = "v0.20.0"
	// renovate: datasource=go depName=github.com/jsonnet-bundler/jsonnet-bundler
	JB_VERSION = "v0.5.1"
	// renovate: datasource=go depName=github.com/brancz/gojsontoyaml
	JSONTOYAML_VERSION = "v0.1.0"
)

type tool struct {
	name     string
	version  string
	fullPath string
}

func (t tool) downloadPath() string {
	return fmt.Sprintf("%s@%s", t.fullPath, t.version)
}

var (
	// goTools is a map of go tool ENV vars to their default binary.
	// It will be run with `go run`.
	// e.g go run github.com/parca-dev/parca-agent/tree/main/cmd/eh-frame@latest.
	goTools = map[string]tool{
		JSONNET: tool{
			name:     "jsonnet",
			version:  JSONNET_VERSION,
			fullPath: "github.com/google/go-jsonnet/cmd/jsonnet",
		},
		JSONNETFMT: tool{
			name:     "jsonnetfmt",
			version:  JSONNET_VERSION,
			fullPath: "github.com/google/go-jsonnet/cmd/jsonnetfmt",
		},
		JB: tool{
			name:     "jb",
			version:  JB_VERSION,
			fullPath: "github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb",
		},
		JSONTOYAML: tool{
			name:     "jsontoyaml",
			version:  JSONTOYAML_VERSION,
			fullPath: "github.com/brancz/gojsontoyaml",
		},
	}
)

// InstallGoTools installs the tools.
func InstallGoTools() error {
	if err := os.MkdirAll(toolsDir, 0700); err != nil {
		return err
	}
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	env := map[string]string{"GOBIN": filepath.Join(wd, toolsDir)}
	args := []string{"install"}
	for _, t := range goTools {
		err := sh.RunWith(env, exe(GO), append(args, t.downloadPath())...)
		if err != nil {
			return err
		}
	}
	return nil
}

// getGoTool returns a runnable go tool.
func checkGoTool(cmd string) func(args ...string) error {
	// if _, err := os.Stat(toolsDir); err == nil {
	// 	return sh.RunCmd(bin(GO), append([]string{"run", filepath.Join(toolsDir, cmd)}, args...)...)
	// }

	if _, err := exec.LookPath(cmd); err != nil {
		panic(fmt.Errorf("binary %s does not exist in PATH: %w", cmd, err))
	}
	return nil
}

// RunGoTool runs the go tool with the given args.
func RunGoTool(cmd string, args ...string) error {
	goRun := sh.RunCmd(exe(GO), "run")
	return goRun(append([]string{goTools[cmd]}, args...)...)
}

// RunGoToolWithOutput runs the go tool with the given args and returns the output.
func RunGoToolWithOutput(cmd string, args ...string) (string, error) {
	goRunOut := sh.OutCmd(exe(GO), "run")
	return goRunOut(append([]string{goTools[cmd]}, args...)...)
}

// GoToolCmd returns a function that runs the go tool with the given args.
func GoToolCmd(cmd string, args ...string) func(args ...string) error {
	return sh.RunCmd(exe(GO), append([]string{"run", goTools[cmd]}, args...)...)
}

// GoToolOutCmd returns a function that runs the go tool with the given args and returns the output.
func GoToolOutCmd(cmd string, args ...string) func(args ...string) (string, error) {
	return sh.OutCmd(exe(GO), append([]string{"run", goTools[cmd]}, args...)...)
}
