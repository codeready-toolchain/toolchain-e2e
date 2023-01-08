package e2e

import (
	"context"
	"strconv"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	current, ok, err := await.GetToolchainCluster(otherAwait.Type, otherAwait.Namespace, nil)
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
		t.Cleanup(func() {
			if err := await.Client.Delete(context.TODO(), toolchainCluster); err != nil && !errors.IsNotFound(err) {
				require.NoError(t, err)
			}
		})

		// when
		err := await.Client.Create(context.TODO(), toolchainCluster)

		// then the ToolchainCluster should be ready
		require.NoError(t, err)
		namedToolchainCluster, err := await.WaitForNamedToolchainClusterWithCondition(toolchainCluster.Name, wait.ReadyToolchainCluster)
		require.NoError(t, err)
		checkAbsenceOfTenantRoleLabel(t, &namedToolchainCluster)
		// other ToolchainCluster should be ready, too
		toolChainClusterWithCondition, err := await.WaitForToolchainClusterWithCondition(otherAwait.Type, otherAwait.Namespace, wait.ReadyToolchainCluster)
		require.NoError(t, err)
		checkAbsenceOfTenantRoleLabel(t, &toolChainClusterWithCondition)
		otherToolchainCluster, err := otherAwait.WaitForToolchainClusterWithCondition(await.Type, await.Namespace, wait.ReadyToolchainCluster)
		require.NoError(t, err)
		checkAbsenceOfTenantRoleLabel(t, &otherToolchainCluster)
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
		t.Cleanup(func() {
			if err := await.Client.Delete(context.TODO(), toolchainCluster); err != nil && !errors.IsNotFound(err) {
				require.NoError(t, err)
			}
		})

		// when
		err := await.Client.Create(context.TODO(), toolchainCluster)
		// then the ToolchainCluster should be offline
		require.NoError(t, err)
		namedToolchainCluster, err := await.WaitForNamedToolchainClusterWithCondition(toolchainCluster.Name, &toolchainv1alpha1.ToolchainClusterCondition{
			Type:   toolchainv1alpha1.ToolchainClusterOffline,
			Status: corev1.ConditionTrue,
		})
		require.NoError(t, err)
		checkAbsenceOfTenantRoleLabel(t, &namedToolchainCluster)
		// other ToolchainCluster should be ready, too
		toolChainClusterWithCondition, err := await.WaitForToolchainClusterWithCondition(otherAwait.Type, otherAwait.Namespace, wait.ReadyToolchainCluster)
		require.NoError(t, err)
		checkAbsenceOfTenantRoleLabel(t, &toolChainClusterWithCondition)
		otherToolchainCluster, err := otherAwait.WaitForToolchainClusterWithCondition(await.Type, await.Namespace, wait.ReadyToolchainCluster)
		require.NoError(t, err)
		checkAbsenceOfTenantRoleLabel(t, &otherToolchainCluster)
	})
}

// checkAbsenceOfTenantRoleLabel covers the scenario in which cluster-role label `tenant` should not be set
// since it's NOT a member cluster type.
func checkAbsenceOfTenantRoleLabel(t *testing.T, toolchainCluster *toolchainv1alpha1.ToolchainCluster) {
	if clusterType, clusterTypeLabelExists := toolchainCluster.Labels[cluster.LabelType]; clusterTypeLabelExists &&
		clusterType != string(cluster.Member) {
		_, clusterRoleHomeLabelFound := toolchainCluster.Labels[cluster.RoleLabel(cluster.Tenant)]
		require.False(t, clusterRoleHomeLabelFound, "invalid label: %s found on toolchaincluster: %s ", cluster.RoleLabel(cluster.Tenant), toolchainCluster.Name)
	}
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

// Type sets the label which defines the type of cluster
func clusterType(t cluster.Type) clusterOption {
	return func(c *toolchainv1alpha1.ToolchainCluster) {
		c.Labels["type"] = string(t)
	}
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
