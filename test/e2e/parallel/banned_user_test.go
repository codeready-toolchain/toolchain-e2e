package parallel

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	testspace "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	testsupportspace "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/stretchr/testify/require"
)

func TestBannedUser(t *testing.T) {
	t.Parallel()

	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	// create an user and a space
	sp, us, _ := testsupportspace.CreateSpaceWithRoleSignupResult(t, awaitilities, "admin",
		testspace.WithSpecTargetCluster(memberAwait.ClusterName),
		testspace.WithTierName("appstudio"),
		testspace.WithStatusProvisionedNamespaces([]toolchainv1alpha1.SpaceNamespace{
			{},
		}),
	)

	// wait until the space has ProvisionedNamespaces
	sp, err := hostAwait.WaitForSpace(t, sp.Name, wait.UntilSpaceHasAnyProvisionedNamespaces())
	require.NoError(t, err)
	
	// ban the user
	_ = CreateBannedUser(t, hostAwait, us.UserSignup.Spec.IdentityClaims.Email)

	// wait until the user is banned
	_, err = hostAwait.
		WithRetryOptions(wait.TimeoutOption(time.Second*10), wait.RetryInterval(time.Second*2)).
		WaitForUserSignup(t, us.UserSignup.Name,
			wait.UntilUserSignupHasConditions(
				wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin(), wait.Banned())...))
	require.NoError(t, err)

	t.Run("banned user cannot list config maps from space", func(t *testing.T) {
		// then
		cms := corev1.ConfigMapList{}

		proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(sp.Name)
		userProxyClient, err := hostAwait.CreateAPIProxyClient(t, us.Token, proxyWorkspaceURL)
		require.NoError(t, err)

		err = userProxyClient.List(context.TODO(), &cms, client.InNamespace(sp.Status.ProvisionedNamespaces[0].Name))
		require.True(t, meta.IsNoMatchError(err), "expected List ConfigMap to return a NoMatch error, actual: %v", err)
	})

	t.Run("banned user cannot create config maps into space", func(t *testing.T) {
		cm := corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cm",
				Namespace: sp.Status.ProvisionedNamespaces[0].Name,
			},
		}
		proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(sp.Name)
		communityUserProxyClient, err := hostAwait.CreateAPIProxyClient(t, us.Token, proxyWorkspaceURL)
		require.NoError(t, err)

		err = communityUserProxyClient.Create(context.TODO(), &cm)
		require.True(t, meta.IsNoMatchError(err), "expected Create ConfigMap to return a NoMatch error, actual: %v", err)
	})
}
