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
	"testing"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestPooledReference(t *testing.T) {
	objFilePool := NewPool(log.NewNopLogger(), prometheus.NewRegistry(), 0) // Should not expire.
	t.Cleanup(func() {
		// There should be root references to release.
		require.NoError(t, objFilePool.Close())
	})

	ref, err := objFilePool.Open("./testdata/fib")
	require.NoError(t, err)
	require.NoError(t, ref.Release())

	ref1, err := objFilePool.Open("./testdata/fib")
	require.NoError(t, err)

	// ref1 is still open, so ref2 should be cloneable.
	ref2 := ref1.MustClone()
	// Releasing ref1 should not release ref2.
	require.NoError(t, ref1.Release())
	require.NoError(t, ref2.Release())
}
