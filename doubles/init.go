package doubles

import (
	"os"
	"testing"

	"github.com/codeready-toolchain/api/pkg/apis"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	userv1 "github.com/openshift/api/user/v1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/operator-framework/operator-sdk/pkg/test/e2eutil"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
)

// InitializeOperators initializes test context, registers schemes and waits until both operators (host, member)
// and corresponding KubeFedCluster CRDs are present, running and ready. Based on the given cluster type
// that represents the current operator that is the target of the e2e test it retrieves namespace names.
// Returns the test context and an instance of Awaitility that contains all necessary information
func InitializeOperators(t *testing.T, obj runtime.Object) (*framework.TestCtx, *wait.Awaitility) {
	schemeBuilder := newSchemeBuilder()
	err := framework.AddToFrameworkScheme(schemeBuilder.AddToScheme, obj)
	require.NoError(t, err, "failed to add custom resource scheme to framework")

	ctx := framework.NewTestCtx(t)

	err = ctx.InitializeClusterResources(CleanupOptions(ctx))
	require.NoError(t, err, "failed to initialize cluster resources")
	t.Log("Initialized cluster resources")

	memberNs := os.Getenv(wait.MemberNsVar)
	hostNs := os.Getenv(wait.HostNsVar)

	// get global framework variables
	f := framework.Global

	// wait for host operator to be ready
	err = e2eutil.WaitForDeployment(t, f.KubeClient, hostNs, "host-operator", 1, wait.OperatorRetryInterval, wait.OperatorTimeout)
	require.NoError(t, err, "failed while waiting for host operator deployment")

	// wait for member operator to be ready
	err = e2eutil.WaitForDeployment(t, f.KubeClient, memberNs, "member-operator", 1, wait.OperatorRetryInterval, wait.OperatorTimeout)
	require.NoError(t, err, "failed while waiting for member operator deployment")

	awaitility := &wait.Awaitility{
		T:                t,
		Client:           f.Client,
		KubeClient:       f.KubeClient,
		ControllerClient: f.Client.Client,
		HostNs:           hostNs,
		MemberNs:         memberNs,
	}

	err = awaitility.WaitForReadyKubeFedClusters()
	require.NoError(t, err)

	t.Log("both operators are ready and in running state")
	return ctx, awaitility
}

func newSchemeBuilder() runtime.SchemeBuilder {
	addToSchemes := append(apis.AddToSchemes, userv1.AddToScheme)
	return addToSchemes
}

// KubeFedLabels takes the label values and returns a key-value map containing label names key and values
func KubeFedLabels(clType cluster.Type, ns, ownerClusterName string) map[string]string {
	labels := map[string]string{}
	if clType != "" {
		labels["type"] = string(clType)
	}
	if ns != "" {
		labels["namespace"] = ns
	}
	labels["ownerClusterName"] = ownerClusterName
	return labels
}
