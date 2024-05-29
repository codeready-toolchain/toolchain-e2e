package parallel

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	. "github.com/codeready-toolchain/toolchain-common/pkg/test/assertions"
	. "github.com/codeready-toolchain/toolchain-common/pkg/test/spaceprovisionerconfig"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/spaceprovisionerconfig"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/util"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpaceProvisionerConfig(t *testing.T) {
	// given
	t.Parallel()

	awaitilities := WaitForDeployments(t)
	host := awaitilities.Host()

	t.Run("ready with existing ready cluster", func(t *testing.T) {
		// given
		// any ToolchainCluster in th host namespace will do. We don't really care...
		cluster, err := host.WaitForToolchainCluster(t)
		require.NoError(t, err)

		// when
		spc := CreateSpaceProvisionerConfig(t, host.Awaitility, ReferencingToolchainCluster(cluster.Name))

		// then
		_, err = wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(Ready()))
		require.NoError(t, err)
	})

	t.Run("not ready without existing cluster", func(t *testing.T) {
		// when
		spc := CreateSpaceProvisionerConfig(t, host.Awaitility, ReferencingToolchainCluster("invalid%@@name"))

		// then
		_, err := wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(NotReady()))
		require.NoError(t, err)
	})
	t.Run("becomes ready when cluster appears and becomes ready", func(t *testing.T) {
		// given
		existingCluster, err := host.WaitForToolchainCluster(t, wait.UntilToolchainClusterHasName(awaitilities.Member1().ClusterName))
		require.NoError(t, err)
		clusterName := util.NewObjectNamePrefix(t) + string(uuid.NewUUID()[0:20])

		// when
		spc := CreateSpaceProvisionerConfig(t, host.Awaitility, ReferencingToolchainCluster(clusterName))

		// then
		_, err = wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(NotReady()))
		require.NoError(t, err)

		// when
		_ = copyClusterWithSecretAndNewName(t, host.Awaitility, clusterName, existingCluster)

		// then
		_, err = wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(Ready()))
		require.NoError(t, err)
	})
	t.Run("becomes not ready when cluster disappears", func(t *testing.T) {
		// given

		// we need to create a copy of the cluster and the token secret
		existingCluster, err := host.WaitForToolchainCluster(t)
		require.NoError(t, err)
		cluster := copyClusterWithSecret(t, host.Awaitility, existingCluster)

		// when
		spc := CreateSpaceProvisionerConfig(t, host.Awaitility, ReferencingToolchainCluster(cluster.Name))

		// then
		_, err = wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(Ready()))
		require.NoError(t, err)

		// when
		assert.NoError(t, host.Client.Delete(context.TODO(), cluster))

		// then
		_, err = wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(NotReady()))
		require.NoError(t, err)
	})
	t.Run("becomes not ready when cluster becomes not ready", func(t *testing.T) {
		// NOTE: this is impossible to test currently because there is no way for a TC to
		// transition from ready to unready (short of updating the TC and restarting the toochaincluster cache controller
		// which is something we can't afford in the parallel tests).
		//
		// // given
		//
		// // we need to create a copy of the cluster and the token secret
		// existingCluster, err := host.WaitForToolchainCluster(t)
		// require.NoError(t, err)
		// cluster := copyClusterWithSecret(t, host.Awaitility, existingCluster)
		//
		// // when
		// spc := CreateSpaceProvisionerConfig(t, host.Awaitility, ReferencingToolchainCluster(cluster.Name))
		//
		// // then
		// _, err = wait.
		// 	For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
		// 	WithNameThat(spc.Name, Is(Ready()))
		// require.NoError(t, err)
		//
		// // when
		// // update the TC such that it is no longer valid.
		// require.NoError(t, host.Client.Get(context.TODO(), client.ObjectKeyFromObject(cluster), cluster))
		// apiEndpoint, err := url.Parse(cluster.Spec.APIEndpoint)
		// require.NoError(t, err)
		// apiEndpoint.Host = apiEndpoint.Hostname() + "-not:" + apiEndpoint.Port()
		// cluster.Spec.APIEndpoint = apiEndpoint.String()
		// require.NoError(t, host.Client.Update(context.TODO(), cluster))
		//
		// // then
		// _, err = wait.
		// 	For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
		// 	WithNameThat(spc.Name, Is(NotReady()))
		// require.NoError(t, err)
	})
}

func copyClusterWithSecret(t *testing.T, a *wait.Awaitility, cluster *toolchainv1alpha1.ToolchainCluster) *toolchainv1alpha1.ToolchainCluster {
	clusterName := util.NewObjectNamePrefix(t) + string(uuid.NewUUID()[0:20])
	return copyClusterWithSecretAndNewName(t, a, clusterName, cluster)
}

func copyClusterWithSecretAndNewName(t *testing.T, a *wait.Awaitility, newName string, cluster *toolchainv1alpha1.ToolchainCluster) *toolchainv1alpha1.ToolchainCluster {
	// copy the secret
	secret := &corev1.Secret{}
	require.NoError(t, a.CopyWithCleanup(t,
		client.ObjectKey{
			Name:      cluster.Spec.SecretRef.Name,
			Namespace: cluster.Namespace,
		},
		client.ObjectKey{
			Name:      newName,
			Namespace: cluster.Namespace,
		},
		secret,
	))

	// and create a new ToolchainCluster with that secret
	// note that we can't use the CopyWithCleanup function because we also
	// need to modify the SecretRef of the TC.
	newCluster := cluster.DeepCopy()
	newCluster.ResourceVersion = ""
	newCluster.UID = ""
	newCluster.Name = newName
	newCluster.Spec.SecretRef.Name = secret.Name
	require.NoError(t, a.CreateWithCleanup(t, newCluster))

	return newCluster
}
