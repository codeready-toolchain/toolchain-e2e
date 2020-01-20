package e2e

import (
	"context"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	"github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/kubefed/pkg/apis/core/common"
	"sigs.k8s.io/kubefed/pkg/apis/core/v1beta1"
	"testing"
)

func TestKubeFedE2E(t *testing.T) {
	fedClusterList := &v1beta1.KubeFedClusterList{}
	ctx, awaitility := testsupport.WaitForDeployments(t, fedClusterList)
	defer ctx.Cleanup()

	verifyKubeFedCluster(ctx, awaitility, cluster.Host, awaitility.Member())
	verifyKubeFedCluster(ctx, awaitility, cluster.Member, awaitility.Host())
}

// verifyKubeFedCluster verifies existence and correct conditions of KubeFedCluster CRD
// in the target cluster type operator
func verifyKubeFedCluster(ctx *test.TestCtx, awaitility *wait.Awaitility, kubeFedClusterType cluster.Type, singleAwait wait.SingleAwaitility) {
	// given
	current, ok, err := singleAwait.GetKubeFedCluster(kubeFedClusterType, nil)
	require.NoError(awaitility.T, err)
	require.True(awaitility.T, ok, "KubeFedCluster should exist")

	awaitility.T.Run("create new KubeFedCluster with correct data and expect to be ready for cluster type "+string(kubeFedClusterType), func(t *testing.T) {
		// given
		name := "new-ready-" + string(kubeFedClusterType)
		fedCluster := singleAwait.NewKubeFedCluster(name,
			wait.Type(kubeFedClusterType),
			wait.APIEndpoint(current.Spec.APIEndpoint),
			wait.CABundle(current.Spec.CABundle),
			wait.SecretRef(current.Spec.SecretRef.Name),
			wait.Owner(current.Labels["ownerClusterName"]),
			wait.Namespace(current.Labels["namespace"]),
			wait.CapacityExhausted, // make sure this cluster cannot be used in other e2e tests
		)

		// when
		err := awaitility.Client.Create(context.TODO(), fedCluster, testsupport.CleanupOptions(ctx))

		// then the KubeFedCluster should be ready
		require.NoError(t, err)
		err = singleAwait.WaitForKubeFedClusterConditionWithName(fedCluster.Name, wait.ReadyKubeFedCluster)
		require.NoError(t, err)
		err = awaitility.WaitForReadyKubeFedClusters()
		require.NoError(t, err)
		err = singleAwait.WaitForKubeFedClusterConditionWithName(current.Name, wait.ReadyKubeFedCluster)
		require.NoError(t, err)
	})
	awaitility.T.Run("create new KubeFedCluster with incorrect data and expect to be offline for cluster type "+string(kubeFedClusterType), func(t *testing.T) {
		// given
		name := "new-offline-" + string(kubeFedClusterType)
		fedCluster := singleAwait.NewKubeFedCluster(name,
			wait.Type(kubeFedClusterType),
			wait.APIEndpoint("https://1.2.3.4:8443"),
			wait.CABundle(current.Spec.CABundle),
			wait.SecretRef(current.Spec.SecretRef.Name),
			wait.Owner(current.Labels["ownerClusterName"]),
			wait.Namespace(current.Labels["namespace"]),
			wait.CapacityExhausted, // make sure this cluster cannot be used in other e2e tests
		)
		// when
		err := awaitility.Client.Create(context.TODO(), fedCluster, testsupport.CleanupOptions(ctx))

		// then the KubeFedCluster should be offline
		require.NoError(t, err)
		err = singleAwait.WaitForKubeFedClusterConditionWithName(fedCluster.Name, &v1beta1.ClusterCondition{
			Type:   common.ClusterOffline,
			Status: corev1.ConditionTrue,
		})
		require.NoError(t, err)
		err = awaitility.WaitForReadyKubeFedClusters()
		require.NoError(t, err)
	})
}
