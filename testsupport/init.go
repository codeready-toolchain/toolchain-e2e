package testsupport

import (
	"os"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	openshiftappsv1 "github.com/openshift/api/apps/v1"
	quotav1 "github.com/openshift/api/quota/v1"
	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	userv1 "github.com/openshift/api/user/v1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	sdkutil "github.com/operator-framework/operator-sdk/pkg/test/e2eutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	metrics "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// WaitForDeployments initializes test context, registers schemes and waits until both operators (host, member)
// and corresponding ToolchainCluster CRDs are present, running and ready. Based on the given cluster type
// that represents the current operator that is the target of the e2e test it retrieves namespace names.
// Also waits for the registration service to be deployed (with 3 replica)
// Returns the test context and an instance of Awaitility that contains all necessary information
func WaitForDeployments(t *testing.T, obj runtime.Object) (*framework.Context, *wait.HostAwaitility, *wait.MemberAwaitility) {
	schemeBuilder := newSchemeBuilder()
	err := framework.AddToFrameworkScheme(schemeBuilder.AddToScheme, obj)
	require.NoError(t, err, "failed to add custom resource to framework scheme")

	ctx := framework.NewContext(t)

	err = ctx.InitializeClusterResources(CleanupOptions(ctx))
	require.NoError(t, err, "failed to initialize cluster resources")
	t.Log("Initialized cluster resources")

	memberNs := os.Getenv(wait.MemberNsVar)
	hostNs := os.Getenv(wait.HostNsVar)
	registrationServiceNs := os.Getenv(wait.RegistrationServiceVar)

	// get global framework variables
	f := framework.Global

	hostAwait := wait.NewHostAwaitility(t, f.Client, f.Client.Client, hostNs, registrationServiceNs)

	// wait for host operator to be ready
	err = sdkutil.WaitForDeployment(t, f.KubeClient, hostNs, "host-operator", 1, wait.DefaultOperatorRetryInterval, wait.DefaultOperatorTimeout)
	require.NoError(t, err, "failed while waiting for host operator deployment")

	// wait for registration service to be ready
	err = sdkutil.WaitForDeployment(t, f.KubeClient, registrationServiceNs, "registration-service", 2, wait.DefaultRetryInterval, wait.DefaultOperatorTimeout)
	require.NoError(t, err, "failed while waiting for registration service deployment")

	registrationServiceRoute, err := hostAwait.WaitForRouteToBeAvailable(registrationServiceNs, "registration-service", "/")
	require.NoError(t, err, "failed while waiting for registration service deployment")

	registrationServiceURL := "http://" + registrationServiceRoute.Spec.Host
	if registrationServiceRoute.Spec.TLS != nil {
		registrationServiceURL = "https://" + registrationServiceRoute.Spec.Host
	}
	hostAwait.RegistrationServiceURL = registrationServiceURL

	// wait for member operator to be ready
	memberCluster, err := hostAwait.WaitForToolchainClusterWithCondition("e2e", memberNs, wait.ReadyToolchainCluster)
	require.NoError(t, err)
	memberConfig, err := cluster.NewClusterConfig(f.Client.Client, &memberCluster, 3*time.Second)
	require.NoError(t, err)

	kubeClient, err := kubernetes.NewForConfig(memberConfig)
	require.NoError(t, err)
	err = sdkutil.WaitForDeployment(t, kubeClient, memberNs, "member-operator", 1, wait.DefaultOperatorRetryInterval, wait.DefaultOperatorTimeout)
	require.NoError(t, err, "failed while waiting for member operator deployment")

	memberClient, err := client.New(memberConfig, client.Options{
		Scheme: f.Scheme,
	})
	require.NoError(t, err)
	memberAwait := wait.NewMemberAwaitility(t, memberClient, memberNs)

	_, err = memberAwait.WaitForToolchainClusterWithCondition(hostAwait.Type, hostAwait.Namespace, wait.ReadyToolchainCluster)
	require.NoError(t, err)

	t.Log("both operators are ready and in running state")
	return ctx, hostAwait, memberAwait
}

func newSchemeBuilder() runtime.SchemeBuilder {
	return append(apis.AddToSchemes,
		userv1.Install,
		templatev1.Install,
		routev1.Install,
		quotav1.Install,
		openshiftappsv1.Install,
		corev1.AddToScheme,
		metrics.AddToScheme,
	)
}
