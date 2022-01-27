package testsupport

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	hasv1alpha1 "github.com/codeready-toolchain/toolchain-e2e/testsupport/has/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/kubectl/pkg/scheme"

	openshiftappsv1 "github.com/openshift/api/apps/v1"
	quotav1 "github.com/openshift/api/quota/v1"
	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	metrics "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	initHostAwait    *wait.HostAwaitility
	initMemberAwait  *wait.MemberAwaitility
	initMember2Await *wait.MemberAwaitility
	initOnce         sync.Once
)

// WaitForDeployments initializes test context, registers schemes and waits until both operators (host, member)
// and corresponding ToolchainCluster CRDs are present, running and ready. Based on the given cluster type
// that represents the current operator that is the target of the e2e test it retrieves namespace names.
// Also waits for the registration service to be deployed (with 3 replica)
// Returns the test context and an instance of Awaitility that contains all necessary information
func WaitForDeployments(t *testing.T) wait.Awaitilities {
	initOnce.Do(func() {
		memberNs := os.Getenv(wait.MemberNsVar)
		memberNs2 := os.Getenv(wait.MemberNsVar2)
		hostNs := os.Getenv(wait.HostNsVar)
		registrationServiceNs := os.Getenv(wait.RegistrationServiceVar)

		apiConfig, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
		require.NoError(t, err)
		kubeconfig, err := clientcmd.NewDefaultClientConfig(*apiConfig, &clientcmd.ConfigOverrides{}).ClientConfig()
		require.NoError(t, err)

		cl, err := client.New(kubeconfig, client.Options{
			Scheme: schemeWithAllAPIs(t),
		})
		require.NoError(t, err)

		initHostAwait = wait.NewHostAwaitility(t, kubeconfig, cl, hostNs, registrationServiceNs)

		// wait for host operator to be ready
		initHostAwait.WaitForDeploymentToGetReady("host-operator-controller-manager", 1)

		// wait for registration service to be ready
		initHostAwait.WaitForDeploymentToGetReady("registration-service", 2)

		// set registration service values
		registrationServiceRoute, err := initHostAwait.WaitForRouteToBeAvailable(registrationServiceNs, "registration-service", "/")
		require.NoError(t, err, "failed while waiting for registration service route")

		registrationServiceURL := "http://" + registrationServiceRoute.Spec.Host
		if registrationServiceRoute.Spec.TLS != nil {
			registrationServiceURL = "https://" + registrationServiceRoute.Spec.Host
		}
		initHostAwait.RegistrationServiceURL = registrationServiceURL

		// set api proxy values
		apiRoute, err := initHostAwait.WaitForRouteToBeAvailable(registrationServiceNs, "api", "/proxyhealth")
		require.NoError(t, err)
		initHostAwait.APIProxyURL = strings.TrimSuffix(fmt.Sprintf("https://%s/%s", apiRoute.Spec.Host, apiRoute.Spec.Path), "/")

		// wait for member operators to be ready
		initMemberAwait = getMemberAwaitility(t, cl, initHostAwait, memberNs)

		initMember2Await = getMemberAwaitility(t, cl, initHostAwait, memberNs2)

		hostToolchainCluster, err := initMemberAwait.WaitForToolchainClusterWithCondition("e2e", hostNs, wait.ReadyToolchainCluster)
		require.NoError(t, err)
		hostConfig, err := cluster.NewClusterConfig(cl, &hostToolchainCluster, 6*time.Second)
		require.NoError(t, err)
		initHostAwait.RestConfig = hostConfig.RestConfig

		// setup host metrics route for metrics verification in tests
		hostMetricsRoute, err := initHostAwait.SetupRouteForService("host-operator-metrics-service", "/metrics")
		require.NoError(t, err)
		initHostAwait.MetricsURL = hostMetricsRoute.Status.Ingress[0].Host

		// setup member metrics route for metrics verification in tests
		memberMetricsRoute, err := initMemberAwait.SetupRouteForService("member-operator-metrics-service", "/metrics")
		require.NoError(t, err, "failed while setting up or waiting for the route to the 'member-operator-metrics' service to be available")
		initMemberAwait.MetricsURL = memberMetricsRoute.Status.Ingress[0].Host

		_, err = initMemberAwait.WaitForToolchainClusterWithCondition(initHostAwait.Type, initHostAwait.Namespace, wait.ReadyToolchainCluster)
		require.NoError(t, err)

		_, err = initMember2Await.WaitForToolchainClusterWithCondition(initHostAwait.Type, initHostAwait.Namespace, wait.ReadyToolchainCluster)
		require.NoError(t, err)

		t.Log("all operators are ready and in running state")
	})

	return wait.NewAwaitilities(initHostAwait.ForTest(t), initMemberAwait.ForTest(t), initMember2Await.ForTest(t))
}

func getMemberAwaitility(t *testing.T, cl client.Client, hostAwait *wait.HostAwaitility, namespace string) *wait.MemberAwaitility {
	memberClusterE2e, err := hostAwait.WaitForToolchainClusterWithCondition("e2e", namespace, wait.ReadyToolchainCluster)
	require.NoError(t, err)
	memberConfig, err := cluster.NewClusterConfig(cl, &memberClusterE2e, 6*time.Second)
	require.NoError(t, err)

	memberClient, err := client.New(memberConfig.RestConfig, client.Options{
		Scheme: schemeWithAllAPIs(t),
	})
	require.NoError(t, err)

	memberCluster, err := hostAwait.WaitForToolchainClusterWithCondition("member", namespace, wait.ReadyToolchainCluster)
	require.NoError(t, err)
	clusterName := memberCluster.Name
	memberAwait := wait.NewMemberAwaitility(t, memberConfig.RestConfig, memberClient, namespace, clusterName)

	memberAwait.WaitForDeploymentToGetReady("member-operator-controller-manager", 1)

	return memberAwait
}

func schemeWithAllAPIs(t *testing.T) *runtime.Scheme {
	s := scheme.Scheme
	builder := append(runtime.SchemeBuilder{}, toolchainv1alpha1.AddToScheme,
		userv1.Install,
		templatev1.Install,
		routev1.Install,
		quotav1.Install,
		openshiftappsv1.Install,
		corev1.AddToScheme,
		metrics.AddToScheme,
		hasv1alpha1.AddToScheme,
	)
	require.NoError(t, builder.AddToScheme(s))
	return s
}
