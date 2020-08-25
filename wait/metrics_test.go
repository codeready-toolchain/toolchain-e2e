package wait

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCounter(t *testing.T) {
	// given
	response := `# HELP go_gc_duration_seconds A summary of the pause duration of garbage collection cycles.
# TYPE go_gc_duration_seconds summary
go_gc_duration_seconds{quantile="0"} 1.4465e-05
go_gc_duration_seconds{quantile="0.25"} 2.1044e-05
go_gc_duration_seconds{quantile="0.5"} 4.6598e-05
go_gc_duration_seconds{quantile="0.75"} 7.1812e-05
go_gc_duration_seconds{quantile="1"} 0.000261445
go_gc_duration_seconds_sum 0.001043916
go_gc_duration_seconds_count 17
# HELP go_goroutines Number of goroutines that currently exist.
# TYPE go_goroutines gauge
go_goroutines 82
controller_runtime_reconcile_total{controller="usersignup-controller",result="success"} 10
workqueue_depth{name="usersignup-controller"} 0
workqueue_depth{name="masteruserrecord-controller"} 0
`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, response)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	// when
	result, err := getCounter(ts.URL, "workqueue_depth", "name", "masteruserrecord-controller")
	// then
	require.NoError(t, err)
	assert.Equal(t, float64(0), result)

}
