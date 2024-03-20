package community_test

import (
	"context"
	"testing"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	waitpoll "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	commonauth "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	authsupport "github.com/codeready-toolchain/toolchain-e2e/testsupport/auth"
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
func TestPublicViewerProxy(t *testing.T) {
	// given

	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	// we create a space to share , a new MUR and a SpaceBindingRequest
	space, _, _ := NewSpaceBindingRequest(t, awaitilities, memberAwait, hostAwait, "admin")

	communityUser := &proxyUser{
		expectedMemberCluster: memberAwait,
		username:              "community-user",
		identityID:            uuid.Must(uuid.NewV4()),
	}
	createAppStudioUser(t, awaitilities, communityUser)

	communityUserProxyClient, err := hostAwait.CreateAPIProxyClient(t, communityUser.token, hostAwait.APIProxyURL)
	require.NoError(t, err)

	t.Run("space is flagged as community", func(t *testing.T) {
		// when
		sb := CreateCommunitySpaceBinding(t, hostAwait, space.Name, space.Namespace)
		require.NoError(t, err)
		t.Logf("created space binding for public-viewer:\n%+v", sb)

		// Wait until space is flagged as community
		require.NoError(t,
			waitpoll.Poll(hostAwait.RetryInterval, hostAwait.Timeout, func() (bool, error) {
				mr, err := labels.NewRequirement(toolchainv1alpha1.SpaceBindingMasterUserRecordLabelKey, selection.In, []string{"public-viewer"})
				if err != nil {
					return false, err
				}

				sr, err := labels.NewRequirement(toolchainv1alpha1.SpaceBindingSpaceLabelKey, selection.Equals, []string{space.Name})
				if err != nil {
					return false, err
				}

				opts := &client.ListOptions{
					Namespace:     space.Namespace,
					LabelSelector: labels.NewSelector().Add(*sr, *mr),
				}
				sbs := &toolchainv1alpha1.SpaceBindingList{}
				if err := hostAwait.Client.List(context.TODO(), sbs, opts); err != nil {
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
				ID:       uuid.Must(uuid.NewV4()),
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
	user.signup, _ = req.Resources()
	user.token = req.GetToken()
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
	return spacebinding.CreateSpaceBindingStr(t, hostAwait, "public-viewer", spaceName, spaceNamespace, "contributor")
}
