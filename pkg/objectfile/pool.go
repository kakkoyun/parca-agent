// Copyright 2022-2023 The Parca Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//

package objectfile

import (
	"debug/elf"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	burrow "github.com/goburrow/cache"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/parca-dev/parca-agent/pkg/buildid"
	"github.com/parca-dev/parca-agent/pkg/cache"
	"github.com/parca-dev/parca-agent/pkg/rc"
)

// Only used as return type for convenience.
type (
	Reference = *rc.Reference[ObjectFile]
	reference = rc.Reference[ObjectFile]
)

type ObjectFile interface {
	Info() *Info
	Reader() (*reader, func() error, error)
	ELF() (*elf.File, error)
}

type Pool struct {
	c burrow.Cache
}

func NewPool(logger log.Logger, reg prometheus.Registerer, profilingDuration time.Duration) *Pool {
	return &Pool{
		c: burrow.New(
			burrow.WithExpireAfterAccess(100*profilingDuration), //nocommit: 10*profilingDuration
			burrow.WithRemovalListener(onRemoval(log.With(logger, "component", "objectfile_pool"))),
			burrow.WithStatsCounter(cache.NewBurrowStatsCounter(logger, reg, "objectfile")),
		),
	}
}

func (p *Pool) Get(buildID string) (Reference, error) {
	if val, ok := p.c.GetIfPresent(buildID); ok {
		val, ok := val.(reference)
		if !ok {
			return nil, fmt.Errorf("unexpected type in cache: %T", val)
		}

		// @nocommit
		return val.MustClone(), nil
		// return val.Clone()
	}

	return nil, fmt.Errorf("no reference found for buildid %s", buildID)
}

// Open opens the specified executable or library file from the given path.
// And creates a new ObjectFile reference.
// The returned reference should be released after use.
// The file will be closed when the reference is released.
func (p *Pool) Open(path string) (Reference, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error opening %s: %w", path, err)
	}
	return p.NewFile(f)
}

// NewFile creates a new ObjectFile reference from an existing file.
// The returned reference should be released after use.
// The file will be closed when the reference is released.
func (p *Pool) NewFile(f *os.File) (Reference, error) {
	closer := func(err error) error {
		if cErr := f.Close(); cErr != nil {
			err = errors.Join(err, cErr)
		}
		return err
	}

	filePath := f.Name()
	ok, err := isELF(f)
	if err != nil {
		return nil, closer(fmt.Errorf("failed check whether file is an ELF file %s: %w", filePath, err))
	}
	if !ok {
		return nil, closer(fmt.Errorf("unrecognized binary format: %s", filePath))
	}
	// > Clients of ReadAt can execute parallel ReadAt calls on the
	//   same input source.
	ef, err := elfNewFile(f) // requires ReaderAt.
	if err != nil {
		return nil, closer(fmt.Errorf("error opening %s: %w", filePath, err))
	}
	if len(ef.Sections) == 0 {
		return nil, closer(errors.New("ELF does not have any sections"))
	}

	buildID := ""
	if id, err := buildid.BuildID(f, ef); err == nil {
		buildID = id
	}
	if rErr := rewind(f); rErr != nil {
		return nil, closer(rErr)
	}

	if val, ok := p.c.GetIfPresent(buildID); ok {
		// A file for this buildID is already in the cache, so close the file we just opened.
		// The existing file could be already closed, because we are done uploading it.
		// It's the callers responsibility to making sure the file is still open.
		if err := closer(nil); err != nil {
			return nil, err
		}
		ref, ok := val.(reference)
		if !ok {
			return nil, fmt.Errorf("unexpected type in cache: %T", val)
		}

		// @nocommit
		return ref.MustClone(), nil
		// return ref.Clone()
	}

	stat, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat the file: %w", err)
	}
	obj := &objectFile{
		mtx:  &sync.Mutex{},
		file: f,
		elf:  ef,

		i: &Info{
			BuildID: buildID,
			Path:    filePath,
			Size:    stat.Size(),
			Modtime: stat.ModTime(),
		},
	}

	ref := rc.New[ObjectFile](obj, obj.close) // TODO(kakkoyun): Invalidate cache when resource is released.
	p.c.Put(buildID, *ref)                    // Obtain a reference for the one we put in the cache.
	// @nocommit
	return ref.MustClone(), nil
	// return ref.Clone()
}

// onRemoval is called when an object file is removed from the cache.
//
// We make sure the file is closed when it's removed from the cache,
// to prevent leaking file descriptors.
// This could create potential issues if there's an ongoing upload for this file.
// This case should be handled by the uploader by re-opening it.
func onRemoval(logger log.Logger) func(key burrow.Key, value burrow.Value) {
	return func(key burrow.Key, value burrow.Value) {
		ref, ok := value.(rc.Reference[ObjectFile])
		if !ok {
			panic(fmt.Errorf("unexpected type in cache: %T", value))
		}
		if err := ref.Release(); err != nil {
			level.Error(logger).Log("msg", "failed to release object file file on removal", "err", err)
			// @nocommit
			panic(err)
		}
	}
}

// Close closes the pool and all the files in it.
func (p *Pool) Close() error {
	// Closing cache will remove all the entries.
	// While removing the entries, the onRemoval function will be called,
	// and the files will be closed.
	return p.c.Close()
}
