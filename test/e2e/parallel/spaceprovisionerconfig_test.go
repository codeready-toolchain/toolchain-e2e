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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		clusterName := util.NewObjectNamePrefix(t) + string(uuid.NewUUID()[0:20])

		// when
		spc := CreateSpaceProvisionerConfig(t, host.Awaitility, ReferencingToolchainCluster(clusterName))

		// then
		_, err := wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(NotReady()))
		require.NoError(t, err)

		// when
		cluster := &toolchainv1alpha1.ToolchainCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterName,
				Namespace: host.Namespace,
			},
		}
		assert.NoError(t, host.CreateWithCleanup(t, cluster))

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
		cluster.Spec.SecretRef.Name = ""
		require.NoError(t, host.Client.Update(context.TODO(), cluster))

		// then
		_, err = wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(NotReady()))
		require.NoError(t, err)
	})
}

func copyClusterWithSecret(t *testing.T, a *wait.Awaitility, cluster *toolchainv1alpha1.ToolchainCluster) *toolchainv1alpha1.ToolchainCluster {
	clusterName := util.NewObjectNamePrefix(t) + string(uuid.NewUUID()[0:20])
	secret := &corev1.Secret{}
	require.NoError(t, a.GetClient().Get(context.TODO(), client.ObjectKey{Name: cluster.Spec.SecretRef.Name, Namespace: cluster.Namespace}, secret))
	secret.ResourceVersion = ""
	secret.UID = ""
	secret.Name = clusterName
	require.NoError(t, a.CreateWithCleanup(t, secret))

	// and create a new ToolchainCluster with that secret
	newCluster := cluster.DeepCopy()
	newCluster.ResourceVersion = ""
	newCluster.UID = ""
	newCluster.Name = clusterName
	newCluster.Spec.SecretRef.Name = secret.Name
	require.NoError(t, a.CreateWithCleanup(t, newCluster))

	return newCluster
}
