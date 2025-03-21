package assertions2

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type AssertT interface {
	assert.TestingT
	Helper()
	Logf(format string, args ...any)
}

type RequireT interface {
	require.TestingT
	Helper()
	Logf(format string, args ...any)
}

type failureTrackingT struct {
	AssertT
	failed bool
}

func (t *failureTrackingT) Errorf(format string, args ...any) {
	t.Helper()
	t.failed = true
	t.AssertT.Errorf(format, args...)
}
