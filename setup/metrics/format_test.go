package metrics

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBytesToMBString(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		t.Run("zero", func(t *testing.T) {
			// given
			var val float64

			// when
			result := bytesToMBString(val)

			require.Equal(t, "0.00 MB", result)
		})

		t.Run("non-zero value", func(t *testing.T) {
			// given
			var val float64 = 123456789

			// when
			result := bytesToMBString(val)

			require.Equal(t, "117.74 MB", result)
		})
	})
}

func TestSimple(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		t.Run("zero", func(t *testing.T) {
			// given
			var val float64

			// when
			result := simple(val)

			require.Equal(t, "0.0000", result)
		})

		t.Run("non-zero value", func(t *testing.T) {
			// given
			val := 123456789.123456789

			// when
			result := simple(val)

			require.Equal(t, "123456789.1235", result)
		})
	})
}
