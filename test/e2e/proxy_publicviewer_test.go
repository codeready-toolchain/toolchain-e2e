package e2e_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	waitpoll "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	commonauth "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	testspace "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	authsupport "github.com/codeready-toolchain/toolchain-e2e/testsupport/auth"
	testsupportspace "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/spacebinding"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
)

type proxyUser struct {
	expectedMemberCluster *wait.MemberAwaitility
	username              string
	token                 string
	identityID            uuid.UUID
	signup                *toolchainv1alpha1.UserSignup
	compliantUsername     string
}

// tests access to community-shared spaces
func TestProxyPublicViewer(t *testing.T) {
	// given

	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	// public viewer is enabled in ToolchainConfig
	hostAwait.UpdateToolchainConfig(t, testconfig.PublicViewerConfig(true))

	// we create a space to share and a user for 'community-user'
	space, _, _ := testsupportspace.CreateSpace(t, awaitilities, testspace.WithTierName("appstudio"), testspace.WithSpecTargetCluster(memberAwait.ClusterName))
	communityUser := &proxyUser{
		expectedMemberCluster: memberAwait,
		username:              "community-user",
		identityID:            uuid.New(),
	}
	createAppStudioUser(t, awaitilities, communityUser)
	communityUserProxyClient, err := hostAwait.CreateAPIProxyClient(t, communityUser.token, hostAwait.APIProxyURL)
	require.NoError(t, err)

	t.Run("space is flagged as community", func(t *testing.T) {
		// when
		sb := CreateCommunitySpaceBinding(t, hostAwait, space.Name, space.Namespace)
		require.NotNil(t, sb)

		// Wait until space is flagged as community
		require.NoError(t,
			waitpoll.Poll(hostAwait.RetryInterval, hostAwait.Timeout, func() (bool, error) {
				opts := []client.ListOption{
					client.MatchingLabels{
						toolchainv1alpha1.SpaceBindingMasterUserRecordLabelKey: toolchainv1alpha1.KubesawAuthenticatedUsername,
						toolchainv1alpha1.SpaceBindingSpaceLabelKey:            space.Name,
					},
					client.InNamespace(space.Namespace),
				}
				sbs := &toolchainv1alpha1.SpaceBindingList{}
				if err := hostAwait.Client.List(context.TODO(), sbs, opts...); err != nil {
					return false, err
				}

				return len(sbs.Items) == 1, nil
			}))

		sp := toolchainv1alpha1.Space{}
		err = hostAwait.Client.Get(context.TODO(), client.ObjectKeyFromObject(space), &sp)
		require.NoError(t, err)

		/*
		   Given Space exists for user A
		   Given User community-user exists
		   When  A flags their space visibility to "community"
		   Then  community-user can view A's Space
		   And   community-user cannot create resources in A's Space
		*/
		t.Run("community user access to community space", func(t *testing.T) {
			require.NotEmpty(t, sp.Status.ProvisionedNamespaces)

			t.Run("community user can list config maps from community space", func(t *testing.T) {
				// then
				cms := corev1.ConfigMapList{}

				communityUserProxyClient, err := hostAwait.CreateAPIProxyClient(t, communityUser.token, hostAwait.ProxyURLWithWorkspaceContext(sp.Name))
				require.NoError(t, err)

				err = communityUserProxyClient.List(context.TODO(), &cms, client.InNamespace(sp.Status.ProvisionedNamespaces[0].Name))
				require.NoError(t, err)
			})

			t.Run("community user cannot create config maps into space", func(t *testing.T) {
				cm := corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cm",
						Namespace: sp.Status.ProvisionedNamespaces[0].Name,
					},
				}
				err := communityUserProxyClient.Create(context.TODO(), &cm)
				require.Error(t, err)
			})
		})

		/*
		   Given Space exists for user A
		   Given SSO user joe exists
		   When  A flags their space visibility to "community"
		   Then  joe can view A's Space
		   And   joe cannot create resources in A's Space
		*/
		t.Run("as sso user", func(t *testing.T) {
			// Given
			userIdentity := &commonauth.Identity{
				ID:       uuid.New(),
				Username: "joe",
			}
			claims := []commonauth.ExtraClaim{commonauth.WithEmailClaim("joe@joe.joe")}
			token, err := authsupport.NewTokenFromIdentity(userIdentity, claims...)
			require.NoError(t, err)

			joeCli, err := hostAwait.CreateAPIProxyClient(t, token, hostAwait.ProxyURLWithWorkspaceContext(sp.Name))
			require.NoError(t, err)

			t.Run("sso user can list config maps from space", func(t *testing.T) {
				// then
				cms := corev1.ConfigMapList{}
				err := joeCli.List(context.TODO(), &cms, client.InNamespace(sp.Status.ProvisionedNamespaces[0].Name))
				require.NoError(t, err)
			})

			t.Run("sso user cannot create config maps into space", func(t *testing.T) {
				// then
				cm := corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cm",
						Namespace: sp.Status.ProvisionedNamespaces[0].Name,
					},
				}
				err := joeCli.Create(context.TODO(), &cm)
				require.Error(t, err)
			})
		})
	})

	t.Run("space is not flagged as community", func(t *testing.T) {
		sp := toolchainv1alpha1.Space{}
		err = hostAwait.Client.Get(context.TODO(), client.ObjectKeyFromObject(space), &sp)
		require.NoError(t, err)

		/*
		   Given Space exists for user A
		   Given User community-user exists
		   Given A space's visibility is NOT "community"
		   Then  community-user cannot view A's Space
		   And   community-user cannot create resources in A's Space
		*/
		t.Run("community user cannot access to non-community space", func(t *testing.T) {
			require.NotEmpty(t, sp.Status.ProvisionedNamespaces)

			t.Run("community user cannot list config maps from community space", func(t *testing.T) {
				// then
				cms := corev1.ConfigMapList{}

				communityUserProxyClient, err := hostAwait.CreateAPIProxyClient(t, communityUser.token, hostAwait.ProxyURLWithWorkspaceContext(sp.Name))
				require.NoError(t, err)

				err = communityUserProxyClient.List(context.TODO(), &cms, client.InNamespace(sp.Status.ProvisionedNamespaces[0].Name))
				require.Error(t, err)
			})

			t.Run("community user cannot create config maps into space", func(t *testing.T) {
				cm := corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-cm",
						Namespace: sp.Status.ProvisionedNamespaces[0].Name,
					},
				}
				err := communityUserProxyClient.Create(context.TODO(), &cm)
				require.Error(t, err)
			})
		})
	})
}

func createAppStudioUser(t *testing.T, awaitilities wait.Awaitilities, user *proxyUser) {
	// Create and approve signup
	req := NewSignupRequest(awaitilities).
		Username(user.username).
		IdentityID(user.identityID).
		ManuallyApprove().
		TargetCluster(user.expectedMemberCluster).
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	user.signup = req.UserSignup
	user.token = req.Token
	tiers.MoveSpaceToTier(t, awaitilities.Host(), user.signup.Status.CompliantUsername, "appstudio")
	VerifyResourcesProvisionedForSignup(t, awaitilities, user.signup, "deactivate30", "appstudio")
	user.compliantUsername = user.signup.Status.CompliantUsername
	_, err := awaitilities.Host().WaitForMasterUserRecord(t, user.compliantUsername, wait.UntilMasterUserRecordHasCondition(wait.Provisioned()))
	require.NoError(t, err)
}

func CreateCommunitySpaceBinding(
	t *testing.T,
	hostAwait *wait.HostAwaitility,
	spaceName, spaceNamespace string,
) *toolchainv1alpha1.SpaceBinding {
	return spacebinding.CreateSpaceBindingStr(t, hostAwait, toolchainv1alpha1.KubesawAuthenticatedUsername, spaceName, spaceNamespace, "viewer")
}
