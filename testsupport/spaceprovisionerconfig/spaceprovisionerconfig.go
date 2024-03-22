package spaceprovisionerconfig

import (
	"context"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testSpc "github.com/codeready-toolchain/toolchain-common/pkg/test/spaceprovisionerconfig"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/util"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func CreateSpaceProvisionerConfig(t *testing.T, await *wait.Awaitility, opts ...testSpc.CreateOption) *toolchainv1alpha1.SpaceProvisionerConfig {
	namePrefix := util.NewObjectNamePrefix(t)

	spc := testSpc.NewSpaceProvisionerConfig("", await.Namespace, opts...)
	spc.GenerateName = namePrefix
	err := await.CreateWithCleanup(t, spc)
	require.NoError(t, err)

	return spc
}

func UpdateForCluster(t *testing.T, await *wait.Awaitility, referencedClusterName string, opts ...testSpc.CreateOption) {
	t.Helper()
	spcs, err := getAllSpcs(t, await)
	require.NoError(t, err)

	spc := findSpcForCluster(spcs, referencedClusterName)
	require.NotNil(t, spc, "could not find SpaceProvisionerConfig referencing the required cluster: %s", referencedClusterName)

	originalSpc := spc.DeepCopy()

	for _, opt := range opts {
		opt(spc)
	}

	require.NoError(t, await.Client.Update(context.TODO(), spc))

	t.Cleanup(func() {
		currentSpc := &toolchainv1alpha1.SpaceProvisionerConfig{}
		err := await.Client.Get(context.TODO(), client.ObjectKeyFromObject(originalSpc), currentSpc)
		if err != nil {
			if errors.IsNotFound(err) {
				require.NoError(t, await.Client.Create(context.TODO(), originalSpc))
				return
			}
			require.Fail(t, err.Error())
		}

		// make the originalSpc look like we freshly obtained it from the server and updated its fields
		// to look like the original.
		originalSpc.Generation = currentSpc.Generation
		originalSpc.ResourceVersion = currentSpc.ResourceVersion

		require.NoError(t, await.Client.Update(context.TODO(), originalSpc))
	})
}

func getAllSpcs(t *testing.T, await *wait.Awaitility) ([]toolchainv1alpha1.SpaceProvisionerConfig, error) {
	t.Helper()
	list := &toolchainv1alpha1.SpaceProvisionerConfigList{}
	if err := await.Client.List(context.TODO(), list, client.InNamespace(await.Namespace)); err != nil {
		return nil, err
	}
	return list.Items, nil
}

func findSpcForCluster(spcs []toolchainv1alpha1.SpaceProvisionerConfig, clusterName string) *toolchainv1alpha1.SpaceProvisionerConfig {
	for _, spc := range spcs {
		if spc.Spec.ToolchainCluster == clusterName {
			return &spc
		}
	}
	return nil
}
