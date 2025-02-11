package testsupport

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	toolchaincommon "github.com/codeready-toolchain/toolchain-common/pkg/client"
	appstudiov1 "github.com/codeready-toolchain/toolchain-e2e/testsupport/appstudio/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/util"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	openshiftappsv1 "github.com/openshift/api/apps/v1"
	quotav1 "github.com/openshift/api/quota/v1"
	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/kubectl/pkg/scheme"
	metrics "k8s.io/metrics/pkg/apis/metrics/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	initHostAwait    *wait.HostAwaitility
	initMemberAwait  *wait.MemberAwaitility
	initMember2Await *wait.MemberAwaitility
	initOnce         sync.Once
)

// WaitForOperators initializes test context, registers schemes and waits until both operators (host, member)
// and corresponding ToolchainCluster CRDs are present, running and ready. Based on the given cluster type that represents the current operator that is the target of
// the e2e test it retrieves namespace names. Also waits for the registration service to be deployed (with 3 replica)
// Returns the test context and an instance of Awaitility that contains all necessary information
func WaitForOperators(t *testing.T) wait.Awaitilities {
	initOnce.Do(func() {
		waitForOperators(t)
	})
	return wait.NewAwaitilities(initHostAwait, initMemberAwait, initMember2Await)
}
func waitForOperators(t *testing.T) {
	memberNs := os.Getenv(wait.MemberNsVar)
	memberNs2 := os.Getenv(wait.MemberNsVar2)
	hostNs := os.Getenv(wait.HostNsVar)
	registrationServiceNs := os.Getenv(wait.RegistrationServiceVar)
	t.Logf("Host Operator namespace: %s", hostNs)
	t.Logf("Member1 Operator namespace: %s", memberNs)
	if IsSecondMemberMode(t) {
		t.Logf("Member2 Operator namespace: %s", memberNs2)
	}
	t.Logf("Registration Service namespace: %s", registrationServiceNs)

	apiConfig, err := clientcmd.NewDefaultClientConfigLoadingRules().Load()
	require.NoError(t, err)

	kubeconfig, err := util.BuildKubernetesRESTConfig(*apiConfig)
	require.NoError(t, err)

	cl, err := client.New(kubeconfig, client.Options{
		Scheme: schemeWithAllAPIs(t),
	})
	require.NoError(t, err)

	//updating the kubeconfig with the bearer token created
	kubeconfig.BearerToken = getE2EServiceAccountToken(t, hostNs, apiConfig, cl)

	initHostAwait = wait.NewHostAwaitility(kubeconfig, cl, hostNs, registrationServiceNs)

	// wait for host operator to be ready
	initHostAwait.WaitForDeploymentToGetReady(t, "host-operator-controller-manager", 1)

	// wait for registration service to be ready
	initHostAwait.WaitForDeploymentToGetReady(t, "registration-service", 3)

	// set registration service values
	registrationServiceRoute, err := initHostAwait.WaitForRouteToBeAvailable(t, registrationServiceNs, "registration-service", "/")
	require.NoError(t, err, "failed while waiting for registration service route")

	registrationServiceURL := "http://" + registrationServiceRoute.Spec.Host
	if registrationServiceRoute.Spec.TLS != nil {
		registrationServiceURL = "https://" + registrationServiceRoute.Spec.Host
	}
	initHostAwait.RegistrationServiceURL = registrationServiceURL

	// wait for member operators to be ready
	initMemberAwait = getMemberAwaitility(t, initHostAwait, kubeconfig, memberNs)

	_, err = initMemberAwait.WaitForToolchainClusterWithCondition(t, initHostAwait.Namespace, toolchainv1alpha1.ConditionReady)
	require.NoError(t, err)

	if IsSecondMemberMode(t) {
		initMember2Await = getMemberAwaitility(t, initHostAwait, kubeconfig, memberNs2)

		_, err = initMember2Await.WaitForToolchainClusterWithCondition(t, initHostAwait.Namespace, toolchainv1alpha1.ConditionReady)
		require.NoError(t, err)
	}
	t.Log("all operators are ready and in running state")
}

func getE2EServiceAccountToken(t *testing.T, hostNs string, apiConfigsa *api.Config, sacl client.Client) string {
	// creating another config which is used for creating only resclient,
	//so that the main kubeconfig is not altered
	restkubeconfig, err := util.BuildKubernetesRESTConfig(*apiConfigsa)
	require.NoError(t, err)

	sa := &corev1.ServiceAccount{}
	err = sacl.Get(context.TODO(), types.NamespacedName{Namespace: hostNs, Name: "e2e-test"}, sa)
	// If not found proceed to create the e2e service account and the cluster role binding
	if errors.IsNotFound(err) {
		t.Logf("No Service Account for e2e test found, proceeding to create it")
		err := sacl.Create(context.TODO(), &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "e2e-test",
				Namespace: hostNs}})
		require.NoError(t, err, "Error in creating Service account for e2e test")
	} else if err != nil {
		require.NoError(t, err, "Error fetching service accounts")
	}

	sacrb := &rbacv1.ClusterRoleBinding{}
	err = sacl.Get(context.TODO(), types.NamespacedName{Name: "e2e-test-cluster-admin"}, sacrb)
	// check if there are any clusterrolebinding present from the previous run of e2e test
	if errors.IsNotFound(err) {
		crb := rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: "e2e-test-cluster-admin",
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "cluster-admin",
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "e2e-test",
					Namespace: hostNs,
				},
			},
		}
		t.Logf("Proceeding to create Cluster Role Binding for the Service Account")
		err = sacl.Create(context.TODO(), &crb)
		require.NoError(t, err, "Error in Creating Cluster role binding")
	} else if err != nil {
		require.NoError(t, err, "Error fetching clusterrolebinding")
	}

	//upating the restkubeconfig ,which requires groupversion to create restclient
	restkubeconfig.ContentConfig =
		rest.ContentConfig{
			GroupVersion:         &authv1.SchemeGroupVersion,
			NegotiatedSerializer: scheme.Codecs,
		}

	//Creating a Restclient to be used in creation and checking of bearer token required for authentication
	rclient, err := rest.RESTClientFor(restkubeconfig)
	require.NoError(t, err, "Error in creating restclient")

	//Creating a bearer token to be used for authentication(which is valid for 24 hrs)
	bt, err := toolchaincommon.CreateTokenRequest(context.TODO(), rclient, types.NamespacedName{Namespace: hostNs, Name: "e2e-test"}, 86400)
	require.NoError(t, err, "Error in creating Token")
	return bt
}

// WaitForDeployments waits for all member Webhooks and autoscaling buffer apps in addition to waiting for
// host and member operators and the registration service to be ready.
//
// The primary reason for separation is because the migration tests are for testing host operator and member operator changes related to Spaces, NSTemplateTiers, etc.
// Webhooks and autoscaling buffers do not deal with the same set of resources so they can be verified independently of migration tests
func WaitForDeployments(t *testing.T) wait.Awaitilities {
	initOnce.Do(func() {
		waitForOperators(t)
		// wait for host and member operators to be ready
		registrationServiceNs := os.Getenv(wait.RegistrationServiceVar)

		// set api proxy values
		apiRoute, err := initHostAwait.WaitForRouteToBeAvailable(t, registrationServiceNs, "api", "/proxyhealth")
		require.NoError(t, err)
		assert.Equal(t, "24h", apiRoute.Annotations["haproxy.router.openshift.io/timeout"])
		initHostAwait.APIProxyURL = strings.TrimSuffix(fmt.Sprintf("https://%s/%s", apiRoute.Spec.Host, apiRoute.Spec.Path), "/")

		// wait for proxy metrics service
		_, err = initHostAwait.WaitForService(t, "proxy-metrics-service")
		require.NoError(t, err, "failed to find proxy metrics service")

		// setup host metrics route for metrics verification in tests
		hostMetricsRoute, err := initHostAwait.SetupRouteForService(t, "host-operator-metrics-service", "/metrics")
		require.NoError(t, err)
		initHostAwait.MetricsURL = hostMetricsRoute.Status.Ingress[0].Host

		// setup member metrics route for metrics verification in tests
		memberMetricsRoute, err := initMemberAwait.SetupRouteForService(t, "member-operator-metrics-service", "/metrics")
		require.NoError(t, err, "failed while setting up or waiting for the route to the 'member-operator-metrics' service to be available")
		initMemberAwait.MetricsURL = memberMetricsRoute.Status.Ingress[0].Host

		// Wait for the webhooks in Member 1 only because we do not deploy webhooks for Member 2
		// (we can't deploy the same webhook multiple times on the same cluster)
		// Also verify the autoscaling buffer in both members
		webhookImage := initMemberAwait.GetContainerEnv(t, "MEMBER_OPERATOR_WEBHOOK_IMAGE")
		require.NotEmpty(t, webhookImage, "The value of the env var MEMBER_OPERATOR_WEBHOOK_IMAGE wasn't found in the deployment of the member operator.")
		if IsSecondMemberMode(t) {
			err = initMember2Await.WaitUntilWebhookDeleted(t) // webhook on member2 should be deleted
			require.NoError(t, err)
		}
		initMemberAwait.WaitForMemberWebhooks(t, webhookImage)

		// wait for autoscaler buffer apps
		initMemberAwait.WaitForAutoscalingBufferApp(t)
		if IsSecondMemberMode(t) {
			initMember2Await.WaitForAutoscalingBufferApp(t)
		}

		// check that the tier exists, and all its namespace other cluster-scoped resource revisions
		// are different from `000000a` which is the value specified in the initial manifest (used for base tier)
		err = initHostAwait.WaitUntilBaseNSTemplateTierIsUpdated(t)
		require.NoError(t, err)

		// check that the default user tier exists and is updated to the current version, an outdated version is applied from deploy/e2e-tests/usertier-base.yaml as
		// part of the e2e test setup make target for the purpose of verifying the user tier update mechanism on startup of the host operator
		err = initHostAwait.WaitUntilBaseUserTierIsUpdated(t)
		require.NoError(t, err)
	})

	if !IsSecondMemberMode(t) {
		return wait.NewAwaitilities(initHostAwait, initMemberAwait)
	}

	return wait.NewAwaitilities(initHostAwait, initMemberAwait, initMember2Await)
}

func getMemberAwaitility(t *testing.T, hostAwait *wait.HostAwaitility, restconfig *rest.Config, namespace string) *wait.MemberAwaitility {
	memberClient, err := client.New(restconfig, client.Options{
		Scheme: schemeWithAllAPIs(t),
	})
	require.NoError(t, err)

	memberCluster, err := hostAwait.WaitForToolchainClusterWithCondition(t, namespace, toolchainv1alpha1.ConditionReady)
	require.NoError(t, err)
	clusterName := memberCluster.Name
	memberAwait := wait.NewMemberAwaitility(restconfig, memberClient, namespace, clusterName)

	memberAwait.WaitForDeploymentToGetReady(t, "member-operator-controller-manager", 1)

	return memberAwait
}

func schemeWithAllAPIs(t *testing.T) *runtime.Scheme {
	s := runtime.NewScheme()
	builder := append(runtime.SchemeBuilder{}, toolchainv1alpha1.AddToScheme,
		userv1.Install,
		templatev1.Install,
		routev1.Install,
		quotav1.Install,
		openshiftappsv1.Install,
		corev1.AddToScheme,
		metrics.AddToScheme,
		appstudiov1.AddToScheme,
		rbacv1.AddToScheme,
		appsv1.AddToScheme,
		schedulingv1.AddToScheme,
		userv1.AddToScheme,
		netv1.AddToScheme,
		admv1.AddToScheme,
	)
	require.NoError(t, builder.AddToScheme(s))
	return s
}

func IsSecondMemberMode(t *testing.T) bool {
	secondMemberMode := os.Getenv(wait.SecondMemberModeVar)
	require.NotEmpty(t, secondMemberMode)

	return secondMemberMode == "true"
}
