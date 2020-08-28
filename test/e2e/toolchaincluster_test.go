package e2e

import (
	"context"
	"strconv"
	"testing"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	"github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestToolchainClusterE2E(t *testing.T) {
	toolchainClusterList := &v1alpha1.ToolchainClusterList{}
	ctx, hostAwait, memberAwait := WaitForDeployments(t, toolchainClusterList)
	defer ctx.Cleanup()

	verifyToolchainCluster(t, ctx, hostAwait.Awaitility, memberAwait.Awaitility)
	verifyToolchainCluster(t, ctx, memberAwait.Awaitility, hostAwait.Awaitility)
}

// verifyToolchainCluster verifies existence and correct conditions of ToolchainCluster CRD
// in the target cluster type operator
func verifyToolchainCluster(t *testing.T, ctx *test.Context, await *wait.Awaitility, otherAwait *wait.Awaitility) {
	// given
	current, ok, err := await.GetToolchainCluster(await.Type, await.Namespace, nil)
	require.NoError(t, err)
	require.True(t, ok, "ToolchainCluster should exist")

	t.Run("create new ToolchainCluster with correct data and expect to be ready for cluster type "+string(await.Type), func(t *testing.T) {
		// given
		name := "new-ready-" + string(await.Type)
		toolchainCluster := newToolchainCluster(await.Namespace, name,
			clusterType(await.Type),
			apiEndpoint(current.Spec.APIEndpoint),
			caBundle(current.Spec.CABundle),
			secretRef(current.Spec.SecretRef.Name),
			owner(current.Labels["ownerClusterName"]),
			namespace(current.Labels["namespace"]),
			capacityExhausted, // make sure this cluster cannot be used in other e2e tests
		)
		defer func() {
			await.Client.Delete(context.TODO(), toolchainCluster)
		}()

		// when
		err := await.Client.Create(context.TODO(), toolchainCluster)

		// then the ToolchainCluster should be ready
		require.NoError(t, err)
		_, err = await.WaitForNamedToolchainClusterWithCondition(toolchainCluster.Name, wait.ReadyToolchainCluster)
		require.NoError(t, err)
		// other ToolchainCluster should be ready, too
		_, err = await.WaitForToolchainClusterWithCondition(otherAwait.Type, otherAwait.Namespace, wait.ReadyToolchainCluster)
		require.NoError(t, err)
		_, err = otherAwait.WaitForToolchainClusterWithCondition(await.Type, await.Namespace, wait.ReadyToolchainCluster)
		require.NoError(t, err)
	})

	t.Run("create new ToolchainCluster with incorrect data and expect to be offline for cluster type "+string(await.Type), func(t *testing.T) {
		// given
		name := "new-offline-" + string(await.Type)
		toolchainCluster := newToolchainCluster(await.Namespace, name,
			clusterType(await.Type),
			apiEndpoint("https://1.2.3.4:8443"),
			caBundle(current.Spec.CABundle),
			secretRef(current.Spec.SecretRef.Name),
			owner(current.Labels["ownerClusterName"]),
			namespace(current.Labels["namespace"]),
			capacityExhausted, // make sure this cluster cannot be used in other e2e tests
		)
		defer func() {
			await.Client.Delete(context.TODO(), toolchainCluster)
		}()

		// when
		err := await.Client.Create(context.TODO(), toolchainCluster)
		// then the ToolchainCluster should be offline
		require.NoError(t, err)
		_, err = await.WaitForNamedToolchainClusterWithCondition(toolchainCluster.Name, &v1alpha1.ToolchainClusterCondition{
			Type:   v1alpha1.ToolchainClusterOffline,
			Status: corev1.ConditionTrue,
		})
		require.NoError(t, err)
		// other ToolchainCluster should be ready, too
		_, err = await.WaitForToolchainClusterWithCondition(otherAwait.Type, otherAwait.Namespace, wait.ReadyToolchainCluster)
		require.NoError(t, err)
		_, err = otherAwait.WaitForToolchainClusterWithCondition(await.Type, await.Namespace, wait.ReadyToolchainCluster)
		require.NoError(t, err)
	})
}

func newToolchainCluster(namespace, name string, options ...clusterOption) *v1alpha1.ToolchainCluster {
	toolchainCluster := &v1alpha1.ToolchainCluster{
		Spec: v1alpha1.ToolchainClusterSpec{
			SecretRef: v1alpha1.LocalSecretReference{
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
type clusterOption func(*v1alpha1.ToolchainCluster)

// capacityExhausted an option to state that the cluster capacity has exhausted
var capacityExhausted clusterOption = func(c *v1alpha1.ToolchainCluster) {
	c.Labels["toolchain.dev.openshift.com/capacity-exhausted"] = strconv.FormatBool(true)
}

// Type sets the label which defines the type of cluster
func clusterType(t cluster.Type) clusterOption {
	return func(c *v1alpha1.ToolchainCluster) {
		c.Labels["type"] = string(t)
	}
}

// Owner sets the 'ownerClusterName' label
func owner(name string) clusterOption {
	return func(c *v1alpha1.ToolchainCluster) {
		c.Labels["ownerClusterName"] = name
	}
}

// Namespace sets the 'namespace' label
func namespace(name string) clusterOption {
	return func(c *v1alpha1.ToolchainCluster) {
		c.Labels["namespace"] = name
	}
}

// SecretRef sets the SecretRef in the cluster's Spec
func secretRef(ref string) clusterOption {
	return func(c *v1alpha1.ToolchainCluster) {
		c.Spec.SecretRef = v1alpha1.LocalSecretReference{
			Name: ref,
		}
	}
}

// APIEndpoint sets the APIEndpoint in the cluster's Spec
func apiEndpoint(url string) clusterOption {
	return func(c *v1alpha1.ToolchainCluster) {
		c.Spec.APIEndpoint = url
	}
}

// CABundle sets the CABundle in the cluster's Spec
func caBundle(bundle string) clusterOption {
	return func(c *v1alpha1.ToolchainCluster) {
		c.Spec.CABundle = bundle
	}
}
