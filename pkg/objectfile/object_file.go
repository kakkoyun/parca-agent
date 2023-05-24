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
	"sync"
	"time"
)

// elfOpen    = elf.Open.
var elfNewFile = elf.NewFile

// ObjectFile represents an executable or library file.
// It handles the lifetime of the underlying file descriptor.
type ObjectFile struct {
	BuildID string

	Path    string
	Size    int64
	Modtime time.Time

	mtx    *sync.Mutex
	closed bool
	file   *os.File
	elf    *elf.File // Opened using elf.NewFile, no need to close.
}

// open opens the specified executable or library file from the given path.
// In normal use, the pool should be used instead of this function.
// This is used to open prematurely closed files.
func (o *ObjectFile) open() error {
	f, err := os.Open(o.Path)
	if err != nil {
		return fmt.Errorf("failed to open file %s: %w", o.Path, err)
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
		return closer(fmt.Errorf("error opening %s: %w", o.Path, err))
	}
	stat, err := f.Stat()
	if err != nil {
		return closer(fmt.Errorf("failed to stat the file: %w", err))
	}
	o.file = f
	o.elf = ef
	o.Size = stat.Size()
	o.Modtime = stat.ModTime()
	return nil
}

// Reader returns a reader for the file.
// Parallel reads are NOT allowed. The caller must call the returned function when done with the reader.
func (o *ObjectFile) Reader() (*os.File, func() error, error) {
	if o.file == nil {
		// This should never happen.
		return nil, nil, fmt.Errorf("file is not initialized")
	}

	o.mtx.Lock()

	reOpened := false
	if o.closed {
		// File is closed, prematurely. Reopen it.
		if err := o.open(); err != nil {
			return nil, nil, fmt.Errorf("failed to reopen the file %s: %w", o.Path, err)
		}
		reOpened = true
	}

	done := func() (ret error) {
		defer o.mtx.Unlock()
		defer func() {
			// The file was already closed, so we should keep it closed.
			if reOpened {
				if err := o.close(); err != nil {
					ret = errors.Join(ret, fmt.Errorf("failed to close the file %s: %w", o.Path, err))
				}
			}
		}()

		// Rewind and make the file for the next reader.
		if err := rewind(o.file); err != nil {
			return fmt.Errorf("failed to seek to the beginning of the file %s while closing: %w", o.Path, err)
		}
		return nil
	}

	// Make sure file is rewound before returning.
	err := rewind(o.file)
	if err == nil {
		return o.file, done, nil
	}
	// Rewind failed with an error.
	err = fmt.Errorf("failed to seek to the beginning of the file %s: %w", o.Path, err)

	if errors.Is(err, os.ErrClosed) {
		// File is closed. This shouldn't have happened while guarded by the mutex. Reopen it.
		if oErr := o.open(); oErr != nil {
			return nil, nil, errors.Join(err, fmt.Errorf("failed to reopen the file %s: %w", o.Path, oErr))
		}
		reOpened = true
	}

	return nil, nil, err
}

func rewind(f io.ReadSeeker) error {
	_, err := f.Seek(0, io.SeekStart)
	return err
}

func (o *ObjectFile) ELF() (_ *elf.File, ret error) {
	if o.elf == nil {
		// This should never happen.
		return nil, fmt.Errorf("elf file is not initialized")
	}

	o.mtx.Lock()
	defer o.mtx.Unlock()

	if o.closed {
		// File is closed, prematurely. Reopen it.
		if err := o.open(); err != nil {
			return nil, fmt.Errorf("failed to reopen the file %s: %w", o.Path, err)
		}
		defer func() {
			// The file was already closed, so we should keep it closed.
			if err := o.close(); err != nil {
				ret = errors.Join(ret, fmt.Errorf("failed to close the file %s: %w", o.Path, err))
			}
		}()
	}
	return o.elf, nil
}

// Close closes the underlying file descriptor.
// It is safe to call this function multiple times.
// File should only be closed once.
func (o *ObjectFile) Close() error {
	if o == nil {
		return nil
	}

	o.mtx.Lock()
	defer o.mtx.Unlock()

	return o.close()
}

func (o *ObjectFile) close() error {
	if o.closed {
		return nil
	}

	var err error
	if o.file != nil {
		err = errors.Join(err, o.file.Close())
		o.closed = true
	}
	return err
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

func (o *ObjectFile) HasTextSection() bool {
	if textSection := o.elf.Section(".text"); textSection == nil {
		return false
	}
	return true
}
