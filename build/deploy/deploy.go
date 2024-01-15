package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/magefile/mage/mg"
	"github.com/magefile/mage/sh"
	"github.com/magefile/mage/target"

	"github.com/parca-dev/parca-agent/build"
	"github.com/parca-dev/parca-agent/build/tools"
	"github.com/parca-dev/parca-agent/build/version"
)

var workingDirectory = filepath.Join(build.WorkingDirectory, "deploy")

var Default = Manifests.All

func findJsonnetFiles() ([]string, error) {
	files := []string{}
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(path, "/vendor/") {
			return nil
		}
		if info.IsDir() {
			for _, ext := range []string{".libsonnet", ".jsonnet"} {
				matches, err := filepath.Glob(filepath.Join(path, fmt.Sprintf("*%s", ext)))
				if err != nil {
					return err
				}
				files = append(files, matches...)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// Format formats the code.
func Format() error {
	if err := ensureWorkingDirectory(); err != nil {
		return err
	}

	jsonnetFiles, err := findJsonnetFiles()
	if err != nil {
		return err
	}

	for _, f := range jsonnetFiles {
		if err := tools.RunGoTool(tools.JSONNETFMT, "-n", "2", "--max-blank-lines", "2", "--string-style", "s", "--comment-style", "s", "-i", f); err != nil {
			return err
		}
	}
	return nil
}

// Vendor installs the vendored dependencies.
func Vendor() error {
	if err := ensureWorkingDirectory(); err != nil {
		return err
	}

	changed, err := target.Dir("vendor", "jsonnetfile.json", "jsonnetfile.lock.json")
	if err != nil {
		return err
	}

	if !changed {
		return nil
	}

	return tools.RunGoTool(tools.JB, "install")
}

type Manifests mg.Namespace

// All generates all the manifests.
func (Manifests) All() error {
	mg.SerialDeps(Vendor, Format)

	if err := ensureWorkingDirectory(); err != nil {
		return err
	}

	agentVersion, err := version.Agent()
	if err != nil {
		return err
	}
	fmt.Println("Agent version:", agentVersion)

	serverVersion, err := version.Server()
	if err != nil {
		return err
	}
	fmt.Println("Server version:", serverVersion)

	mg.Deps(Manifests.Tilt, Manifests.Kubernetes, Manifests.OpenShift)
	return nil
}

// Tilt generates development manifests to be used with tilt.
func (Manifests) Tilt() error {
	mg.SerialDeps(Vendor, Format)

	if err := ensureWorkingDirectory(); err != nil {
		return err
	}

	if err := sh.Rm("tilt"); err != nil {
		return err
	}
	if err := os.MkdirAll("tilt", 0o755); err != nil {
		return err
	}

	if err := tools.RunGoTool(tools.JSONNET, "-J", "vendor", "-m", "manifests", "tilt.jsonnet"); err != nil {
		return err
	}
	return nil
}

// Kubernetes generates the manifests to be used with kubernetes.
func (Manifests) Kubernetes() error {
	mg.SerialDeps(Vendor, Format)

	if err := ensureWorkingDirectory(); err != nil {
		return err
	}

	if err := sh.Rm("manifests/kubernetes"); err != nil {
		return err
	}
	if err := os.MkdirAll("manifests/kubernetes", 0o755); err != nil {
		return err
	}

	if err := tools.RunGoTool(tools.JSONNET, "-J", "vendor", "-m", "manifests", "kubernetes.jsonnet"); err != nil {
		return err
	}
	return nil
}

// OpenShift generates the manifests to be used with openshift.
func (Manifests) OpenShift() error {
	mg.SerialDeps(Vendor, Format)

	if err := ensureWorkingDirectory(); err != nil {
		return err
	}

	if err := sh.Rm("manifests/openshift"); err != nil {
		return err
	}
	if err := os.MkdirAll("manifests/openshift", 0o755); err != nil {
		return err
	}

	if err := tools.RunGoTool(tools.JSONNET, "-J", "vendor", "-m", "manifests", "openshift.jsonnet"); err != nil {
		return err
	}
	return nil
}

func ensureWorkingDirectory() error {
	pwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	if pwd == workingDirectory {
		return nil
	}
	if err := os.Chdir(workingDirectory); err != nil {
		return fmt.Errorf("failed to change directory %s: %w", workingDirectory, err)
	}
	return nil
}
