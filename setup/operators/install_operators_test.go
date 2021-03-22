package operators

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/setup/test"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestEnsureAllNamespacesOperator(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		t.Run("operator not installed", func(t *testing.T) {
			// given
			cl := test.NewFakeClient(t) // csv does not exist
			var checked bool
			// the first list is empty which indicates the operator is not installed, thereby triggering the installation
			// the second list returns a CSV which indicates the operator is installed
			cl.MockList = func(ctx context.Context, list runtime.Object, opts ...client.ListOption) error {
				if checked {
					csvList := list.(*v1alpha1.ClusterServiceVersionList)
					csvList.Items = append(csvList.Items, v1alpha1.ClusterServiceVersion{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "kiali-operator",
							Namespace: "test-ns",
						},
					})
					return nil
				}

				checked = true
				return nil
			}

			// when
			err := EnsureAllNamespacesOperator(cl, "test-ns")

			// then
			require.NoError(t, err)
		})
		t.Run("operator already installed", func(t *testing.T) {
			// given
			csv := &v1alpha1.ClusterServiceVersion{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "kiali-operator-v1",
					Namespace: "test-ns",
				},
			}
			cl := test.NewFakeClient(t, csv) // csv exists

			// when
			err := EnsureAllNamespacesOperator(cl, "test-ns")

			// then
			require.NoError(t, err)
		})
	})

	t.Run("failures", func(t *testing.T) {
		t.Run("error when checking if operator is installed", func(t *testing.T) {
			// given
			cl := test.NewFakeClient(t)
			cl.MockList = func(ctx context.Context, list runtime.Object, opts ...client.ListOption) error {
				return fmt.Errorf("Test client error")
			}

			// when
			err := EnsureAllNamespacesOperator(cl, "test-ns")

			// then
			require.EqualError(t, err, "Test client error")
		})

		t.Run("error when creating subscription", func(t *testing.T) {
			// given
			cl := test.NewFakeClient(t)
			cl.MockCreate = func(ctx context.Context, list runtime.Object, opts ...client.CreateOption) error {
				return fmt.Errorf("Test client error")
			}

			// when
			err := EnsureAllNamespacesOperator(cl, "test-ns")

			// then
			require.EqualError(t, err, "failed to install all-namespaces operator: Test client error")
		})

		t.Run("error when waiting for CSV to be created", func(t *testing.T) {
			// given
			cl := test.NewFakeClient(t)
			var checked bool
			// first time no CSV, second time return error
			cl.MockList = func(ctx context.Context, list runtime.Object, opts ...client.ListOption) error {
				if checked {
					return fmt.Errorf("Test client error")
				}
				checked = true
				return nil
			}

			// when
			err := EnsureAllNamespacesOperator(cl, "test-ns")

			// then
			require.EqualError(t, err, `could not find the expected CSV with prefix 'kiali-operator' in namespace 'test-ns': Test client error`)
		})

		t.Run("timed out waiting for CSV to be created", func(t *testing.T) {
			// given
			configuration.DefaultTimeout = time.Second * 1
			cl := test.NewFakeClient(t) // csv does not exist

			// when
			err := EnsureAllNamespacesOperator(cl, "test-ns")

			// then
			require.EqualError(t, err, `could not find the expected CSV with prefix 'kiali-operator' in namespace 'test-ns': timed out waiting for the condition`)
		})
	})
}
