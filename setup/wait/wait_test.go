package wait_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/setup/test"
	"github.com/codeready-toolchain/toolchain-e2e/setup/wait"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestWaitForNamespace(t *testing.T) {

	t.Run("success", func(t *testing.T) {
		// given
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "user0001-stage",
			},
		}
		cl := test.NewFakeClient(t, ns) // ns exists

		// when
		err := wait.WaitForNamespace(cl, "user0001-stage")

		// then
		require.NoError(t, err)
	})

	t.Run("failures", func(t *testing.T) {

		t.Run("timeout", func(t *testing.T) {
			// given
			configuration.DefaultTimeout = time.Second * 1
			cl := test.NewFakeClient(t) // ns doesn't exist

			// when
			err := wait.WaitForNamespace(cl, "user0001-missing")

			// then
			require.Error(t, err)
			assert.EqualError(t, err, "namespace 'user0001-missing' does not exist: timed out waiting for the condition")
		})

	})
}

func TestHasCSVWithPrefix(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		// given
		csv := &v1alpha1.ClusterServiceVersion{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-prefix",
				Namespace: "test-ns",
			},
		}
		cl := test.NewFakeClient(t, csv) // csv exists

		// when
		res, err := wait.HasCSVWithCondition(cl, "test-prefix", "test-ns")

		// then
		require.NoError(t, err)
		require.True(t, res)
	})

	t.Run("failures", func(t *testing.T) {
		t.Run("csv does not exist", func(t *testing.T) {
			// given
			cl := test.NewFakeClient(t) // csv does not exist

			// when
			res, err := wait.HasCSVWithCondition(cl, "test-prefix", "test-ns")

			// then
			require.NoError(t, err)
			require.False(t, res)
		})

		t.Run("client error", func(t *testing.T) {
			// given
			cl := test.NewFakeClient(t) // csv does not exist
			cl.MockList = func(ctx context.Context, list runtime.Object, opts ...client.ListOption) error {
				return fmt.Errorf("Test client error")
			}

			// when
			res, err := wait.HasCSVWithCondition(cl, "test-prefix", "test-ns")

			// then
			require.EqualError(t, err, "Test client error")
			require.False(t, res)
		})
	})
}

func TestWaitForCSVWithCondition(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		// given
		csv := &v1alpha1.ClusterServiceVersion{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-prefix",
				Namespace: "test-ns",
			},
		}
		cl := test.NewFakeClient(t, csv) // csv exists

		// when
		err := wait.WaitForCSVWithCondition(cl, "test-prefix", "test-ns")

		// then
		require.NoError(t, err)
	})

	t.Run("failures", func(t *testing.T) {
		t.Run("csv does not exist", func(t *testing.T) {
			// given
			configuration.DefaultTimeout = time.Second * 1
			cl := test.NewFakeClient(t) // csv does not exist

			// when
			err := wait.WaitForCSVWithCondition(cl, "test-prefix", "test-ns")

			// then
			require.EqualError(t, err, `could not find the expected CSV with prefix 'test-prefix' in namespace 'test-ns': timed out waiting for the condition`)
		})

		t.Run("client error", func(t *testing.T) {
			// given
			cl := test.NewFakeClient(t) // csv does not exist
			cl.MockList = func(ctx context.Context, list runtime.Object, opts ...client.ListOption) error {
				return fmt.Errorf("Test client error")
			}

			// when
			err := wait.WaitForCSVWithCondition(cl, "test-prefix", "test-ns")

			// then
			require.EqualError(t, err, `could not find the expected CSV with prefix 'test-prefix' in namespace 'test-ns': Test client error`)
		})
	})
}
