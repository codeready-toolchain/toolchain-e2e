package operators

import (
	"context"
	"fmt"
	"testing"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/setup/test"
	"github.com/operator-framework/api/pkg/operators/v1alpha1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEnsureOperatorsInstalled(t *testing.T) {
	csvTimeout = time.Millisecond
	scheme, err := configuration.NewScheme()
	require.NoError(t, err)

	t.Run("success", func(t *testing.T) {
		t.Run("operator not installed", func(t *testing.T) {
			// given
			cl := test.NewFakeClient(t)
			cl.MockGet = func(ctx context.Context, key types.NamespacedName, obj client.Object) error {
				if sub, ok := obj.(*v1alpha1.Subscription); ok {
					sub.Status.CurrentCSV = "kiali-operator.v1.24.7" // set CurrentCSV to simulate a good subscription
					return nil
				}

				if csv, ok := obj.(*v1alpha1.ClusterServiceVersion); ok {
					kialiCSV := kialiCSV(v1alpha1.CSVPhaseSucceeded)
					kialiCSV.DeepCopyInto(csv)
					return nil
				}
				return cl.Client.Get(ctx, key, obj)
			}

			// when
			err = EnsureOperatorsInstalled(cl, scheme, []string{"installtemplates/kiali.yaml"})

			// then
			require.NoError(t, err)
		})
	})

	t.Run("failures", func(t *testing.T) {
		configuration.DefaultTimeout = 1 * time.Millisecond
		configuration.DefaultRetryInterval = 1 * time.Millisecond

		t.Run("error when creating subscription", func(t *testing.T) {
			// given
			cl := test.NewFakeClient(t)
			cl.MockCreate = func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
				if obj.GetObjectKind().GroupVersionKind().Kind == "Subscription" {
					return fmt.Errorf("Test client error")
				}
				return cl.Client.Create(ctx, obj, opts...)
			}

			// when
			err := EnsureOperatorsInstalled(cl, scheme, []string{"installtemplates/kiali.yaml"})

			// then
			require.EqualError(t, err, "could not apply resource 'kiali-ossm' in namespace 'openshift-operators': unable to create resource of kind: Subscription, version: v1alpha1: Test client error")
		})
		t.Run("error when getting subscription", func(t *testing.T) {
			// given
			cl := test.NewFakeClient(t)
			count := 0
			cl.MockGet = func(ctx context.Context, key types.NamespacedName, obj client.Object) error {
				if obj.GetObjectKind().GroupVersionKind().Kind == "Subscription" {
					if count > 1 { // ignore the first call because it's called from the applyclient
						return fmt.Errorf("Test client error")
					}
					count++
				}
				return cl.Client.Get(ctx, key, obj)
			}

			// when
			err := EnsureOperatorsInstalled(cl, scheme, []string{"installtemplates/kiali.yaml"})

			// then
			require.EqualError(t, err, "failed to verify installation of operator with subscription 'kiali-ossm': could not find a Subscription with name 'kiali-ossm' in namespace 'openshift-operators' that meets the expected criteria: timed out waiting for the condition")
		})

		t.Run("error when getting csv", func(t *testing.T) {
			// given
			cl := test.NewFakeClient(t)
			cl.MockGet = func(ctx context.Context, key types.NamespacedName, obj client.Object) error {
				if sub, ok := obj.(*v1alpha1.Subscription); ok {
					sub.Status.CurrentCSV = "kiali-operator.v1.24.7" // set CurrentCSV to simulate a good subscription
					return nil
				}

				if obj.GetObjectKind().GroupVersionKind().Kind == "ClusterServiceVersion" {
					return fmt.Errorf("Test client error")
				}
				return cl.Client.Get(ctx, key, obj)
			}

			// when
			err := EnsureOperatorsInstalled(cl, scheme, []string{"installtemplates/kiali.yaml"})

			// then
			require.EqualError(t, err, "failed to find CSV 'kiali-operator.v1.24.7' with Phase 'Succeeded': could not find a CSV with name 'kiali-operator.v1.24.7' in namespace 'openshift-operators' that meets the expected criteria: timed out waiting for the condition")
		})

		t.Run("csv has wrong phase", func(t *testing.T) {
			// given
			cl := test.NewFakeClient(t)
			cl.MockGet = func(ctx context.Context, key types.NamespacedName, obj client.Object) error {
				if sub, ok := obj.(*v1alpha1.Subscription); ok {
					sub.Status.CurrentCSV = "kiali-operator.v1.24.7" // set CurrentCSV to simulate a good subscription
					return nil
				}

				if csv, ok := obj.(*v1alpha1.ClusterServiceVersion); ok {
					kialiCSV := kialiCSV(v1alpha1.CSVPhaseFailed)
					kialiCSV.DeepCopyInto(csv)
					return nil
				}
				return cl.Client.Get(ctx, key, obj)
			}

			// when
			err = EnsureOperatorsInstalled(cl, scheme, []string{"installtemplates/kiali.yaml"})

			// then
			require.EqualError(t, err, "failed to find CSV 'kiali-operator.v1.24.7' with Phase 'Succeeded': could not find a CSV with name 'kiali-operator.v1.24.7' in namespace 'openshift-operators' that meets the expected criteria: timed out waiting for the condition")
		})

		t.Run("no subscription in template", func(t *testing.T) {
			// given
			cl := test.NewFakeClient(t)

			// when
			err := EnsureOperatorsInstalled(cl, scheme, []string{"../test/installtemplates/badoperator.yaml"})

			// then
			require.EqualError(t, err, "a subscription was not found in template file '../test/installtemplates/badoperator.yaml'")
		})
	})
}

func kialiCSV(phase v1alpha1.ClusterServiceVersionPhase) *v1alpha1.ClusterServiceVersion {
	return &v1alpha1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kiali-operator.v1.24.7",
			Namespace: "openshift-operators",
		},
		Spec: v1alpha1.ClusterServiceVersionSpec{},
		Status: v1alpha1.ClusterServiceVersionStatus{
			Phase: phase,
		},
	}
}
