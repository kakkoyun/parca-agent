// Package version provides functions to obtain VCS information about the Parca components.
package version

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/parca-dev/parca-agent/build"
)

const (
	parcaReleaseURL = "https://api.github.com/repos/parca-dev/parca/releases/latest"

	unknownVersion = "unknown"
)

var (
	httpTimeout = time.Second * 5

	agentVersion  *string
	serverVersion *string
)

// Agent is the version of the agent.
func Agent() (string, error) {
	if agentVersion != nil {
		return *agentVersion, nil
	}

	repo, err := git.PlainOpen(filepath.Join(build.WorkingDirectory, ".git"))
	if err != nil {
		return unknownVersion, fmt.Errorf("failed to open git repository. %s: %w", build.WorkingDirectory, err)
	}

	ref, err := repo.Head()
	if err != nil {
		return unknownVersion, err
	}

	tag, err := repo.TagObject(ref.Hash())
	if err == nil {
		version := tag.Name
		agentVersion = &version
		return version, nil
	}

	version := fmt.Sprintf("%s-%s", ref.Name().Short(), ref.Hash().String()[:8])
	agentVersion = &version
	return version, nil
}

type payload struct {
	TagName string `json:"tag_name"` //nolint: tagliatelle
}

// Server is the version of the server.
func Server() (string, error) {
	if serverVersion != nil {
		return *serverVersion, nil
	}

	version := unknownVersion
	req, err := http.NewRequest(http.MethodGet, parcaReleaseURL, nil)
	if err != nil {
		return version, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), httpTimeout)
	defer cancel()

	resp, err := http.DefaultClient.Do(req.WithContext(ctx))
	if err != nil {
		return version, err
	}
	defer resp.Body.Close()

	var p payload
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return version, err
	}
	version = p.TagName

	serverVersion = &version
	return version, nil
}
