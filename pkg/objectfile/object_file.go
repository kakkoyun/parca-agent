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

// This package includes modified code from the github.com/google/pprof/internal/binutils

package objectfile

import (
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"
)

// elfOpen    = elf.Open.
var elfNewFile = elf.NewFile

type Info struct {
	BuildID string

	Path    string
	Size    int64
	Modtime time.Time

	// If exists, must be released while closing the file.
	DebugFile Reference
}

// objectFile represents an executable or library file.
// It handles the lifetime of the underlying file descriptor.
type objectFile struct {
	i *Info

	mtx      *sync.Mutex
	file     *os.File
	elf      *elf.File // Opened using elf.NewFile, no need to close.
	closed   bool
	closedBy *runtime.Frames // Stack trace of the first Close call.
}

// reader is a wrapper around os.File that implements io.ReaderAt, io.Seeker and io.Reader.
// It is used to ensure that the file is not closed while it is being used.
type reader struct {
	f *os.File
}

func (r *reader) Read(p []byte) (int, error) {
	return r.f.Read(p)
}

func (r *reader) ReadAt(p []byte, off int64) (int, error) {
	return r.f.ReadAt(p, off)
}

func (r *reader) Seek(offset int64, whence int) (int64, error) {
	return r.f.Seek(offset, whence)
}

var (
	ErrNotInitialized = errors.New("file is not initialized")
	ErrAlreadyClosed  = errors.New("file is already closed")
)

func (o *objectFile) Info() *Info {
	return o.i
}

// Reader returns a reader for the file.
// Parallel reads are NOT allowed. The caller must call the returned function when done with the reader.
func (o *objectFile) Reader() (*reader, func() error, error) {
	if o.file == nil {
		// This should never happen.
		return nil, nil, ErrNotInitialized
	}

	o.mtx.Lock()

	reOpened := false
	if o.closed {
		// File is closed, prematurely. Reopen it.
		if err := o.reopen(); err != nil {
			return nil, nil, fmt.Errorf("failed to reopen the file %s: %w", o.i.Path, err)
		}
		reOpened = true
	}

	done := func() (ret error) {
		defer o.mtx.Unlock()
		defer func() {
			// The file was already closed, so we should keep it closed.
			if reOpened {
				if err := o._close(); err != nil {
					ret = errors.Join(ret, fmt.Errorf("failed to close the file %s: %w", o.i.Path, err))
				}
			}
		}()

		// Rewind and make the file for the next reader.
		if err := rewind(o.file); err != nil {
			return fmt.Errorf("failed to seek to the beginning of the file %s while closing: %w", o.i.Path, err)
		}
		return nil
	}

	// Make sure file is rewound before returning.
	err := rewind(o.file)
	if err == nil {
		return &reader{o.file}, done, nil
	}
	// Rewind failed with an error.
	err = fmt.Errorf("failed to seek to the beginning of the file %s: %w", o.i.Path, err)

	if errors.Is(err, os.ErrClosed) {
		// File is closed. This shouldn't have happened while guarded by the mutex. Reopen it.
		if oErr := o.reopen(); oErr != nil {
			return nil, nil, errors.Join(err, fmt.Errorf("failed to reopen the file %s: %w", o.i.Path, oErr))
		}
		reOpened = true
	}

	return nil, nil, err
}

// ELF returns the ELF file for the object file.
// Parallel reads are allowed.
func (o *objectFile) ELF() (_ *elf.File, ret error) {
	if o.elf == nil {
		// This should never happen.
		return nil, fmt.Errorf("elf file is not initialized")
	}

	if o.closed {
		o.mtx.Lock()
		defer o.mtx.Unlock()

		// File is closed, prematurely. Reopen it.
		if err := o.reopen(); err != nil {
			return nil, fmt.Errorf("failed to reopen the file %s: %w", o.i.Path, err)
		}
		defer func() {
			// The file was already closed, so we should keep it closed.
			if err := o._close(); err != nil {
				ret = errors.Join(ret, fmt.Errorf("failed to close the file %s: %w", o.i.Path, err))
			}
		}()
	}
	return o.elf, nil
}

// close closes the underlying file descriptor.
// It is safe to call this function multiple times.
// File should only be closed once.
func (o *objectFile) close() error {
	if o == nil {
		return nil
	}

	o.mtx.Lock()
	defer o.mtx.Unlock()

	return o._close()
}

func (o *objectFile) _close() error {
	if o.closed {
		return errors.Join(ErrAlreadyClosed, fmt.Errorf("file %s is already closed by: %s", o.i.Path, frames(o.closedBy)))
	}

	var err error
	if o.file != nil {
		err = errors.Join(err, o.file.Close())
		o.closed = true
		o.closedBy = callers()
	}
	if o.i.DebugFile != nil {
		// @nocommit
		o.i.DebugFile.MustRelease()
		// err = errors.Join(err, o.i.DebugFile.Release())
		o.i.DebugFile = nil
	}
	return err
}

func rewind(f io.ReadSeeker) error {
	_, err := f.Seek(0, io.SeekStart)
	return err
}

// @nocommit: Eliminate reopening. Test with panics.

// reopen opens the specified executable or library file from the given path.
// In normal use, the pool should be used instead of this function.
// This is used to open prematurely closed files.
func (o *objectFile) reopen() error {
	f, err := os.Open(o.i.Path)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", o.i.Path, err)
	}
	closer := func(err error) error {
		if cErr := f.Close(); cErr != nil {
			err = errors.Join(err, cErr)
		}
		return err
	}
	// > Clients of ReadAt can execute parallel ReadAt calls on the
	//   same input source.
	ef, err := elfNewFile(f) // requires ReaderAt.
	if err != nil {
		return closer(fmt.Errorf("error opening %s: %w", o.i.Path, err))
	}
	stat, err := f.Stat()
	if err != nil {
		return closer(fmt.Errorf("failed to stat the file: %w", err))
	}
	o.file = f
	o.elf = ef
	o.i.Size = stat.Size()
	o.i.Modtime = stat.ModTime()
	return nil
}

// isELF opens a file to check whether its format is ELF.
func isELF(f *os.File) (_ bool, err error) {
	defer func() {
		if rErr := rewind(f); rErr != nil {
			err = errors.Join(err, rErr)
		}
	}()

	// Read the first 4 bytes of the file.
	var header [4]byte
	if _, err := f.Read(header[:]); err != nil {
		return false, fmt.Errorf("error reading magic number from %s: %w", f.Name(), err)
	}

	// Match against supported file types.
	isELFMagic := string(header[:]) == elf.ELFMAG
	return isELFMagic, nil
}

func callers() *runtime.Frames {
	pcs := make([]uintptr, 20)
	n := runtime.Callers(1, pcs)
	if n == 0 {
		return nil
	}
	return runtime.CallersFrames(pcs[:n])
}

func frames(frames *runtime.Frames) string {
	builder := strings.Builder{}
	for {
		frame, more := frames.Next()
		if strings.Contains(frame.File, "runtime/") {
			break
		}
		builder.WriteString(fmt.Sprintf("%s (%s:%d) /", frame.Function, frame.File, frame.Line))
		if !more {
			break
		}
	}
	return builder.String()
}
