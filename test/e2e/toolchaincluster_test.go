package e2e

import (
	"context"
	"testing"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	"github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
)

func TestToolchainClusterE2E(t *testing.T) {
	toolchainClusterList := &v1alpha1.ToolchainClusterList{}
	ctx, awaitility := WaitForDeployments(t, toolchainClusterList)
	defer ctx.Cleanup()

	verifyToolchainCluster(ctx, awaitility, cluster.Host, awaitility.Member().SingleAwaitility)
	verifyToolchainCluster(ctx, awaitility, cluster.Member, awaitility.Host().SingleAwaitility)
}

// verifyToolchainCluster verifies existence and correct conditions of ToolchainCluster CRD
// in the target cluster type operator
func verifyToolchainCluster(ctx *test.Context, awaitility *wait.Awaitility, toolchainClusterType cluster.Type, singleAwait *wait.SingleAwaitility) {
	// given
	current, ok, err := singleAwait.GetToolchainCluster(toolchainClusterType, nil)
	require.NoError(awaitility.T, err)
	require.True(awaitility.T, ok, "ToolchainCluster should exist")

	awaitility.T.Run("create new ToolchainCluster with correct data and expect to be ready for cluster type "+string(toolchainClusterType), func(t *testing.T) {
		// given
		name := "new-ready-" + string(toolchainClusterType)
		toolchainCluster := singleAwait.NewToolchainCluster(name,
			wait.Type(toolchainClusterType),
			wait.APIEndpoint(current.Spec.APIEndpoint),
			wait.CABundle(current.Spec.CABundle),
			wait.SecretRef(current.Spec.SecretRef.Name),
			wait.Owner(current.Labels["ownerClusterName"]),
			wait.Namespace(current.Labels["namespace"]),
			wait.CapacityExhausted, // make sure this cluster cannot be used in other e2e tests
		)

		// when
		err := awaitility.Client.Create(context.TODO(), toolchainCluster, CleanupOptions(ctx))

		// then the ToolchainCluster should be ready
		require.NoError(t, err)
		err = singleAwait.WaitForToolchainClusterConditionWithName(toolchainCluster.Name, wait.ReadyToolchainCluster)
		require.NoError(t, err)
		err = awaitility.WaitForReadyToolchainClusters()
		require.NoError(t, err)
		err = singleAwait.WaitForToolchainClusterConditionWithName(current.Name, wait.ReadyToolchainCluster)
		require.NoError(t, err)
	})
	awaitility.T.Run("create new ToolchainCluster with incorrect data and expect to be offline for cluster type "+string(toolchainClusterType), func(t *testing.T) {
		// given
		name := "new-offline-" + string(toolchainClusterType)
		toolchainCluster := singleAwait.NewToolchainCluster(name,
			wait.Type(toolchainClusterType),
			wait.APIEndpoint("https://1.2.3.4:8443"),
			wait.CABundle(current.Spec.CABundle),
			wait.SecretRef(current.Spec.SecretRef.Name),
			wait.Owner(current.Labels["ownerClusterName"]),
			wait.Namespace(current.Labels["namespace"]),
			wait.CapacityExhausted, // make sure this cluster cannot be used in other e2e tests
		)
		// when
		err := awaitility.Client.Create(context.TODO(), toolchainCluster, CleanupOptions(ctx))

		// then the ToolchainCluster should be offline
		require.NoError(t, err)
		err = singleAwait.WaitForToolchainClusterConditionWithName(toolchainCluster.Name, &v1alpha1.ToolchainClusterCondition{
			Type:   v1alpha1.ToolchainClusterOffline,
			Status: corev1.ConditionTrue,
		})
		require.NoError(t, err)
		err = awaitility.WaitForReadyToolchainClusters()
		require.NoError(t, err)
	})
}
