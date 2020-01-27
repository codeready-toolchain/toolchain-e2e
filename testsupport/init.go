package testsupport

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	userv1 "github.com/openshift/api/user/v1"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	"github.com/operator-framework/operator-sdk/pkg/test/e2eutil"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
)

// WaitForDeployments initializes test context, registers schemes and waits until both operators (host, member)
// and corresponding KubeFedCluster CRDs are present, running and ready. Based on the given cluster type
// that represents the current operator that is the target of the e2e test it retrieves namespace names.
// Also waits for the registration service to be deployed (with 3 replica)
// Returns the test context and an instance of Awaitility that contains all necessary information
func WaitForDeployments(t *testing.T, obj runtime.Object) (*framework.TestCtx, *wait.Awaitility) {
	schemeBuilder := newSchemeBuilder()
	err := framework.AddToFrameworkScheme(schemeBuilder.AddToScheme, obj)
	require.NoError(t, err, "failed to add custom resource scheme to framework")

	ctx := framework.NewTestCtx(t)

	err = ctx.InitializeClusterResources(CleanupOptions(ctx))
	require.NoError(t, err, "failed to initialize cluster resources")
	t.Log("Initialized cluster resources")

	memberNs := os.Getenv(wait.MemberNsVar)
	hostNs := os.Getenv(wait.HostNsVar)
	registrationServiceNs := os.Getenv(wait.RegistrationServiceVar)

	// get global framework variables
	f := framework.Global

	// wait for host operator to be ready
	err = e2eutil.WaitForDeployment(t, f.KubeClient, hostNs, "host-operator", 1, wait.DefaultOperatorRetryInterval, wait.DefaultOperatorTimeout)
	require.NoError(t, err, "failed while waiting for host operator deployment")

	// wait for member operator to be ready
	err = e2eutil.WaitForDeployment(t, f.KubeClient, memberNs, "member-operator", 1, wait.DefaultOperatorRetryInterval, wait.DefaultOperatorTimeout)
	require.NoError(t, err, "failed while waiting for member operator deployment")

	// wait for registration service to be ready
	err = e2eutil.WaitForDeployment(t, f.KubeClient, registrationServiceNs, "registration-service", 2, wait.DefaultRetryInterval, wait.DefaultOperatorTimeout)
	require.NoError(t, err, "failed while waiting for registration service deployment")

	registrationServiceRoute, err := waitForRoute(t, f, registrationServiceNs, "registration-service", wait.DefaultRetryInterval, wait.DefaultOperatorTimeout)
	require.NoError(t, err, "failed while waiting for registration service deployment")

	registrationServiceURL := "http://" + registrationServiceRoute.Spec.Host
	if registrationServiceRoute.Spec.TLS != nil {
		registrationServiceURL = "https://" + registrationServiceRoute.Spec.Host
	}
	awaitility := &wait.Awaitility{
		T:                      t,
		Client:                 f.Client,
		KubeClient:             f.KubeClient,
		ControllerClient:       f.Client.Client,
		Scheme:                 f.Scheme,
		HostNs:                 hostNs,
		MemberNs:               memberNs,
		RegistrationServiceNs:  registrationServiceNs,
		RegistrationServiceURL: registrationServiceURL,
	}

	err = awaitility.WaitForReadyKubeFedClusters()
	require.NoError(t, err)

	t.Log("both operators are ready and in running state")
	return ctx, awaitility
}

// waitForRoute
func waitForRoute(t *testing.T, f *framework.Framework, ns, name string, retryInterval time.Duration, timeout time.Duration) (routev1.Route, error) {
	var route routev1.Route
	// retrieve the route for the registration service
	err := k8swait.Poll(retryInterval, timeout, func() (done bool, err error) {
		err = f.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: ns,
				Name:      name,
			},
			&route)
		if err != nil {
			if apierrors.IsNotFound(err) {
				t.Logf("Waiting for availability of route '%s' in namespace '%s'...\n", name, ns)
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	return route, err
}

func newSchemeBuilder() runtime.SchemeBuilder {
	addToSchemes := append(apis.AddToSchemes, userv1.AddToScheme)
	addToSchemes = append(addToSchemes, templatev1.AddToScheme)
	addToSchemes = append(addToSchemes, routev1.AddToScheme)
	return addToSchemes
}
