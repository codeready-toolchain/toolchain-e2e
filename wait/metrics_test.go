package wait

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const response = `# HELP go_gc_duration_seconds A summary of the pause duration of garbage collection cycles.
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
# HELP sandbox_master_user_record_current Current number of Master User Records
# TYPE sandbox_master_user_record_current gauge
sandbox_master_user_record_current 7
# HELP sandbox_user_signups_auto_deactivated_total Total number of Automatically Deactivated User Signups
# TYPE sandbox_user_signups_auto_deactivated_total counter
sandbox_user_signups_auto_deactivated_total 0
# HELP sandbox_user_signups_banned_total Total number of Banned User Signups
# TYPE sandbox_user_signups_banned_total counter
sandbox_user_signups_banned_total 0
# HELP sandbox_user_signups_deactivated_total Total number of Deactivated User Signups
# TYPE sandbox_user_signups_deactivated_total counter
sandbox_user_signups_deactivated_total 0
# HELP sandbox_user_signups_provisioned_total Total number of Provisioned User Signups
# TYPE sandbox_user_signups_provisioned_total counter
sandbox_user_signups_provisioned_total 7
# HELP sandbox_user_signups_total Total number of unique User Signups
# TYPE sandbox_user_signups_total counter
sandbox_user_signups_total 7
`

func TestGetMetricValue(t *testing.T) {
	// given
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintln(w, response)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	url := strings.TrimPrefix(ts.URL, "https://")

	t.Run("valid metrics", func(t *testing.T) {
		t.Run("counter with no labels", func(t *testing.T) {
			// when
			result, err := getMetricValue(url, "sandbox_user_signups_total", []string{})
			// then
			require.NoError(t, err)
			assert.Equal(t, float64(7), result)
		})

		t.Run("counter with single label", func(t *testing.T) {
			// when
			result, err := getMetricValue(url, "workqueue_depth", []string{"name", "masteruserrecord-controller"})
			// then
			require.NoError(t, err)
			assert.Equal(t, float64(0), result)
		})

		t.Run("counter with two labels", func(t *testing.T) {
			// when
			result, err := getMetricValue(url, "controller_runtime_reconcile_total", []string{"controller", "usersignup-controller", "result", "success"})
			// then
			require.NoError(t, err)
			assert.Equal(t, float64(10), result)
		})

		t.Run("gauge with no labels", func(t *testing.T) {
			// when
			result, err := getMetricValue(url, "sandbox_master_user_record_current", []string{})
			// then
			require.NoError(t, err)
			assert.Equal(t, float64(7), result)
		})
	})

	t.Run("failures", func(t *testing.T) {
		t.Run("metric does not exist", func(t *testing.T) {
			// when
			result, err := getMetricValue(url, "non_existent_counter", []string{})
			// then
			require.Error(t, err)
			require.EqualError(t, err, "Metric 'non_existent_counter{[]}' not found")
			assert.Equal(t, float64(-1), result)
		})

		t.Run("metric family exists but labels do not match", func(t *testing.T) {
			// when
			result, err := getMetricValue(url, "workqueue_depth", []string{"name", "non-existent-controller"})
			// then
			require.Error(t, err)
			require.EqualError(t, err, "Metric 'workqueue_depth{[name non-existent-controller]}' not found")
			assert.Equal(t, float64(-1), result)
		})

		t.Run("odd number of label parameters", func(t *testing.T) {
			// when
			result, err := getMetricValue(url, "workqueue_depth", []string{"name"})
			// then
			require.Error(t, err)
			require.EqualError(t, err, "Received odd number of label arguments, labels must be key-value pairs")
			assert.Equal(t, float64(-1), result)
		})
	})
}
