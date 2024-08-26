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
	t.Run("becomes ready when cluster becomes ready", func(t *testing.T) {
		// given
		existingCluster, err := host.WaitForToolchainCluster(t, wait.UntilToolchainClusterHasName(awaitilities.Member1().ClusterName))
		require.NoError(t, err)
		tc := copyClusterWithoutSecret(t, host.Awaitility, existingCluster)
		spc := CreateSpaceProvisionerConfig(t, host.Awaitility, ReferencingToolchainCluster(tc.Name))

		_, err = wait.
			For(t, host.Awaitility, &toolchainv1alpha1.SpaceProvisionerConfig{}).
			WithNameThat(spc.Name, Is(NotReady()))
		require.NoError(t, err)

		newSecretName := util.NewObjectNamePrefix(t) + string(uuid.NewUUID()[0:20])
		wait.CopyWithCleanup(t, host.Awaitility,
			client.ObjectKey{Name: existingCluster.Spec.SecretRef.Name, Namespace: existingCluster.Namespace},
			client.ObjectKey{Name: newSecretName, Namespace: existingCluster.Namespace},
			&corev1.Secret{})

		// when
		_, err = host.UpdateToolchainCluster(t, tc.Name, func(updatedTc *toolchainv1alpha1.ToolchainCluster) {
			updatedTc.Spec.SecretRef.Name = newSecretName
		})
		require.NoError(t, err)

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
}

func copyClusterWithSecret(t *testing.T, a *wait.Awaitility, cluster *toolchainv1alpha1.ToolchainCluster) *toolchainv1alpha1.ToolchainCluster {
	t.Helper()
	clusterName := util.NewObjectNamePrefix(t) + string(uuid.NewUUID()[0:20])

	// copy the secret
	secret := &corev1.Secret{}
	wait.CopyWithCleanup(t, a,
		client.ObjectKey{
			Name:      cluster.Spec.SecretRef.Name,
			Namespace: cluster.Namespace,
		},
		client.ObjectKey{
			Name:      clusterName,
			Namespace: cluster.Namespace,
		},
		secret,
	)

	// and copy the cluster referencing the new secret
	newCluster := &toolchainv1alpha1.ToolchainCluster{}
	wait.CopyWithCleanup(t, a,
		client.ObjectKeyFromObject(cluster),
		client.ObjectKey{Name: clusterName, Namespace: cluster.Namespace},
		newCluster,
		func(tc *toolchainv1alpha1.ToolchainCluster) {
			tc.Spec.SecretRef.Name = secret.Name
			tc.Status = toolchainv1alpha1.ToolchainClusterStatus{}
		})

	return newCluster
}

func copyClusterWithoutSecret(t *testing.T, a *wait.Awaitility, cluster *toolchainv1alpha1.ToolchainCluster) *toolchainv1alpha1.ToolchainCluster {
	t.Helper()
	newName := util.NewObjectNamePrefix(t) + string(uuid.NewUUID()[0:20])
	newCluster := &toolchainv1alpha1.ToolchainCluster{}
	wait.CopyWithCleanup(t, a,
		client.ObjectKeyFromObject(cluster),
		client.ObjectKey{Name: newName, Namespace: cluster.Namespace},
		newCluster,
		func(tc *toolchainv1alpha1.ToolchainCluster) {
			tc.Spec.SecretRef.Name = ""
			tc.Status = toolchainv1alpha1.ToolchainClusterStatus{}
		})

	return newCluster
}
