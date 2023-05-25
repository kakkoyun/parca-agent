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

package rc

import (
	"errors"
	"runtime"
	"sync"

	"go.uber.org/atomic"
)

// This package provides a mechanism for managing the life cycle of a resource manually using reference counting.
// e.g. When a file is evicted from the memory, it ensures that the file isn't removed from disk until all active users have completed their operations.
//
// While similar functionality can be achieved using runtime.SetFinalizer,
// this package takes a more explicit approach towards handling references, which not only provides greater clarity,
// but also gives ability to avoid the overhead of allocation/opening costly resources repeatedly.

// This package provides full concurrent safety. All operations are wait-free, ensuring smooth execution even with multiple concurrent accesses.
// One of the key guarantees is that the closer/destructor, which handles the removal of references, is called only once, and that too only when no live references remain.
// This is executed synchronously upon the release of the final reference.

// Furthermore, it ensures that references can be released only once, preventing potential duplication errors.
// Importantly, no new references can be created once the closer/destructor has run its course, preserving the integrity of the process.
// Another feature is that if Clone function is called post the execution of Release function, the cloning process will fail.

var (
	ErrReleased      = errors.New("reference already released")
	ErrAlreadyClosed = errors.New("resource already closed")
)

type resource[T any] struct {
	refCount *atomic.Int32

	val T
	// This function intentionally excluded from the interface.
	// The value type should not expose the closer/destructor except through the reference.
	mtx    *sync.Mutex
	closed bool
	closer func() error
}

func (r *resource[T]) Inc() int32 {
	return r.refCount.Inc()
}

func (r *resource[T]) Dec() int32 {
	return r.refCount.Dec()
}

func (r *resource[T]) Value() T {
	r.mtx.Lock()
	if r.closed {
		r.mtx.Unlock()
		panic(ErrAlreadyClosed)
	}
	r.mtx.Unlock()

	return r.val
}

func (r *resource[T]) Close() error {
	if r.closer == nil {
		return nil
	}

	r.mtx.Lock()
	defer r.mtx.Unlock()

	if err := r.closer(); err != nil {
		return err
	}
	r.closed = true
	return nil
}

type Reference[T any] struct {
	// The type T should be a pointer type.
	resource *resource[T]
	released *atomic.Bool
}

func New[T any](val T, closer func() error) *Reference[T] {
	return newReference(newResource(val, closer))
}

func newReference[T any](res *resource[T]) *Reference[T] {
	ref := &Reference[T]{res, atomic.NewBool(false)}
	// See https://pkg.go.dev/runtime#SetFinalizer.
	runtime.SetFinalizer(ref, func(ref *Reference[T]) error {
		// This is a fail-safe mechanism to ensure that the closer/destructor is called,
		// even if the reference is not released manually.
		return ref.Release()
	})
	return ref
}

func newResource[T any](val T, closer func() error) *resource[T] {
	res := &resource[T]{atomic.NewInt32(0), val, &sync.Mutex{}, false, closer}
	defer res.Inc()
	// See https://pkg.go.dev/runtime#SetFinalizer.
	runtime.SetFinalizer(res, func(res *resource[T]) error {
		// This is a fail-safe mechanism to ensure that the closer is called,
		// even if the reference is not released manually.
		return res.closer()
	})

	return res
}

func (r *Reference[T]) Clone() (*Reference[T], error) {
	if r.released.Load() {
		return nil, ErrReleased
	}
	r.resource.Inc()
	return newReference(r.resource), nil
}

func (r *Reference[T]) MustClone() *Reference[T] {
	ref, err := r.Clone()
	if err != nil {
		panic(err)
	}
	return ref
}

func (r *Reference[T]) Release() error {
	if !r.released.CompareAndSwap(false, true) {
		return ErrReleased
	}
	if r.resource.Dec() == 0 {
		return r.resource.Close()
	}
	return nil
}

func (r *Reference[T]) MustRelease() {
	if !r.released.CompareAndSwap(false, true) {
		panic(ErrReleased)
	}
	if r.resource.Dec() == 0 {
		if err := r.resource.Close(); err != nil {
			panic(err)
		}
	}
}

// Value intentionally panics to prevent accidental use of the value after the reference is released.
func (r *Reference[T]) Value() T {
	if r.released.Load() {
		panic(ErrReleased)
	}
	return r.resource.Value()
}
