package e2e

import (
	"context"
	"strconv"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestToolchainClusterE2E(t *testing.T) {
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	verifyToolchainCluster(t, hostAwait.Awaitility, memberAwait.Awaitility)
	verifyToolchainCluster(t, memberAwait.Awaitility, hostAwait.Awaitility)
}

// verifyToolchainCluster verifies existence and correct conditions of ToolchainCluster CRD
// in the target cluster type operator
func verifyToolchainCluster(t *testing.T, await *wait.Awaitility, otherAwait *wait.Awaitility) {
	// given
	current, ok, err := await.GetToolchainCluster(t, otherAwait.Namespace, nil)
	require.NoError(t, err)
	require.True(t, ok, "ToolchainCluster should exist")

	t.Run("create new ToolchainCluster with correct data and expect to be ready for cluster ", func(t *testing.T) {
		// given
		name := "new-ready-"
		toolchainCluster := newToolchainCluster(await.Namespace, name,
			apiEndpoint(current.Spec.APIEndpoint),
			caBundle(current.Spec.CABundle),
			secretRef(current.Spec.SecretRef.Name),
			owner(current.Labels["ownerClusterName"]),
			namespace(current.Labels["namespace"]),
			capacityExhausted, // make sure this cluster cannot be used in other e2e tests
		)
		t.Cleanup(func() {
			if err := await.Client.Delete(context.TODO(), toolchainCluster); err != nil && !errors.IsNotFound(err) {
				require.NoError(t, err)
			}
		})

		// when
		err := await.Client.Create(context.TODO(), toolchainCluster)
		require.NoError(t, err)
		// wait for toolchaincontroller to reconcile
		time.Sleep(1 * time.Second)

		// then the ToolchainCluster should be ready
		_, err = await.WaitForToolchainCluster(t,
			wait.UntilToolchainClusterHasName(toolchainCluster.Name),
			wait.UntilToolchainClusterHasCondition(*wait.ReadyToolchainCluster),
		)
		require.NoError(t, err)
		// other ToolchainCluster should be ready, too
		_, err = await.WaitForToolchainCluster(t,
			wait.UntilToolchainClusterHasLabels(
				client.MatchingLabels{
					"namespace": otherAwait.Namespace,
				},
			), wait.UntilToolchainClusterHasCondition(*wait.ReadyToolchainCluster),
		)
		require.NoError(t, err)
		_, err = otherAwait.WaitForToolchainCluster(t,
			wait.UntilToolchainClusterHasLabels(client.MatchingLabels{
				"namespace": await.Namespace,
			}), wait.UntilToolchainClusterHasCondition(*wait.ReadyToolchainCluster),
		)
		require.NoError(t, err)
	})

	t.Run("create new ToolchainCluster with incorrect data and expect to be offline for cluster type ", func(t *testing.T) {
		// given
		name := "new-offline-"
		toolchainCluster := newToolchainCluster(await.Namespace, name,
			apiEndpoint("https://1.2.3.4:8443"),
			caBundle(current.Spec.CABundle),
			secretRef(current.Spec.SecretRef.Name),
			owner(current.Labels["ownerClusterName"]),
			namespace(current.Labels["namespace"]),
			capacityExhausted, // make sure this cluster cannot be used in other e2e tests
		)
		t.Cleanup(func() {
			if err := await.Client.Delete(context.TODO(), toolchainCluster); err != nil && !errors.IsNotFound(err) {
				require.NoError(t, err)
			}
		})

		// when
		err := await.Client.Create(context.TODO(), toolchainCluster)
		// wait for toolchaincontroller to reconcile
		time.Sleep(1 * time.Second)

		// then the ToolchainCluster should be offline
		require.NoError(t, err)
		_, err = await.WaitForToolchainCluster(t,
			wait.UntilToolchainClusterHasName(toolchainCluster.Name),
			wait.UntilToolchainClusterHasCondition(toolchainv1alpha1.ToolchainClusterCondition{
				Type:   toolchainv1alpha1.ToolchainClusterOffline,
				Status: corev1.ConditionTrue,
			}),
		)
		require.NoError(t, err)
		// other ToolchainCluster should be ready, too
		_, err = await.WaitForToolchainCluster(t,
			wait.UntilToolchainClusterHasLabels(
				client.MatchingLabels{
					"namespace": otherAwait.Namespace,
				},
			), wait.UntilToolchainClusterHasCondition(*wait.ReadyToolchainCluster),
		)
		require.NoError(t, err)
		_, err = otherAwait.WaitForToolchainCluster(t,
			wait.UntilToolchainClusterHasLabels(client.MatchingLabels{
				"namespace": await.Namespace,
			}), wait.UntilToolchainClusterHasCondition(*wait.ReadyToolchainCluster),
		)
		require.NoError(t, err)
	})
}

func newToolchainCluster(namespace, name string, options ...clusterOption) *toolchainv1alpha1.ToolchainCluster {
	toolchainCluster := &toolchainv1alpha1.ToolchainCluster{
		Spec: toolchainv1alpha1.ToolchainClusterSpec{
			SecretRef: toolchainv1alpha1.LocalSecretReference{
				Name: "", // default
			},
			APIEndpoint: "", // default
			CABundle:    "", // default
		},
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Labels: map[string]string{
				"type":             "member",
				"ownerClusterName": "east",
			},
		},
	}
	for _, configure := range options {
		configure(toolchainCluster)
	}
	return toolchainCluster
}

// clusterOption an option to configure the cluster to use in the tests
type clusterOption func(*toolchainv1alpha1.ToolchainCluster)

// capacityExhausted an option to state that the cluster capacity has exhausted
var capacityExhausted clusterOption = func(c *toolchainv1alpha1.ToolchainCluster) {
	c.Labels["toolchain.dev.openshift.com/capacity-exhausted"] = strconv.FormatBool(true)
}

// Owner sets the 'ownerClusterName' label
func owner(name string) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Labels["ownerClusterName"] = name
	}
}

// Namespace sets the 'namespace' label
func namespace(name string) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Labels["namespace"] = name
	}
}

// SecretRef sets the SecretRef in the cluster's Spec
func secretRef(ref string) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Spec.SecretRef = toolchainv1alpha1.LocalSecretReference{
			Name: ref,
		}
	}
}

// APIEndpoint sets the APIEndpoint in the cluster's Spec
func apiEndpoint(url string) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Spec.APIEndpoint = url
	}
}

// CABundle sets the CABundle in the cluster's Spec
func caBundle(bundle string) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Spec.CABundle = bundle
	}
}
