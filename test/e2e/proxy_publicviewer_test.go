package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	commonauth "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	testspace "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	authsupport "github.com/codeready-toolchain/toolchain-e2e/testsupport/auth"
	testsupportspace "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/spacebinding"
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
	hostAwait.UpdateToolchainConfig(t, testconfig.PublicViewerConfig(false))

	// we create a space to share
	space, _, _ := testsupportspace.CreateSpace(t, awaitilities,
		testspace.WithTierName("appstudio"),
		testspace.WithSpecTargetCluster(memberAwait.ClusterName),
	)

	// test cases for community user
	tt := map[string]struct {
		communityUserOpts            []func(sr *SignupRequest) *SignupRequest
		additionalHostResources      []client.Object
		userSignupRequiredConditions []toolchainv1alpha1.Condition
	}{
		"approved user with space": {
			communityUserOpts: []func(sr *SignupRequest) *SignupRequest{
				func(sr *SignupRequest) *SignupRequest {
					return sr.
						ManuallyApprove().
						EnsureMUR().
						RequireConditions(
							wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...)
				},
			},
		},
		"approved user without space": {
			communityUserOpts: []func(sr *SignupRequest) *SignupRequest{
				func(sr *SignupRequest) *SignupRequest {
					return sr.
						ManuallyApprove().
						EnsureMUR().
						NoSpace().
						RequireConditions(
							wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...)
				},
			},
		},
		"user is banned": {
			communityUserOpts: []func(sr *SignupRequest) *SignupRequest{
				func(sr *SignupRequest) *SignupRequest {
					return sr.
						ManuallyApprove().
						EnsureMUR().
						RequireConditions(
							wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...)
				},
			},
			additionalHostResources: []client.Object{
				NewBannedUser(hostAwait, "communityuser@teste2e.com"),
			},
			userSignupRequiredConditions: wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin(), wait.Banned()),
		},
		"not approved user": {
			communityUserOpts: []func(sr *SignupRequest) *SignupRequest{
				func(sr *SignupRequest) *SignupRequest {
					return sr.
						NoSpace().
						RequireConditions(
							wait.ConditionSet(wait.Default(), wait.VerificationRequired())...)
				},
			},
		},
	}

	t.Run("space is flagged as community", func(t *testing.T) {
		// when
		sb := CreateCommunitySpaceBinding(t, hostAwait, space.Name, space.Namespace)
		require.NotNil(t, sb)

		// Wait until space is flagged as community
		_, err := hostAwait.WaitForSpaceBinding(t, toolchainv1alpha1.KubesawAuthenticatedUsername, space.Name, wait.UntilSpaceBindingHasSpaceRole("viewer"))
		require.NoError(t, err)

		// wait until the space has ProvisionedNamespaces
		sp, err := hostAwait.WaitForSpace(t, space.Name, wait.UntilSpaceHasAnyProvisionedNamespaces())
		require.NoError(t, err)

		/*
		   Given Space exists for user A
		   Given User community-user exists
		   When  A flags their space visibility to "community"
		   Then  community-user can view A's Space
		   And   community-user cannot create resources in A's Space
		*/
		t.Run("community user access to community space", func(t *testing.T) {
			for s, c := range tt {
				t.Run(s, func(t *testing.T) {
					// create community user
					communityUser := createAppStudioCommunityUser(t, awaitilities, memberAwait, c.communityUserOpts...)

					// create additional resources
					for _, r := range c.additionalHostResources {
						err := hostAwait.CreateWithCleanup(t, r)
						require.NoError(t, err)
					}

					// if set, wait until the user signup has the required conditions
					if c.userSignupRequiredConditions != nil {
						_, err := hostAwait.
							WithRetryOptions(wait.TimeoutOption(time.Second*10), wait.RetryInterval(time.Second*2)).
							WaitForUserSignup(t, communityUser.UserSignup.Name, wait.UntilUserSignupHasConditions(c.userSignupRequiredConditions...))
						require.NoError(t, err)
					}

					t.Run("community user can list config maps from community space", func(t *testing.T) {
						// then
						cms := corev1.ConfigMapList{}

						proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(sp.Name)
						communityUserProxyClient, err := hostAwait.CreateAPIProxyClient(t, communityUser.Token, proxyWorkspaceURL)
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
						proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(sp.Name)
						communityUserProxyClient, err := hostAwait.CreateAPIProxyClient(t, communityUser.Token, proxyWorkspaceURL)
						require.NoError(t, err)

						err = communityUserProxyClient.Create(context.TODO(), &cm)
						require.True(t, errors.IsForbidden(err), "expected Create ConfigMap as community user to return a Forbidden error, actual: %v", err)
					})
				})
			}
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
				require.True(t, errors.IsForbidden(err), "expected Create ConfigMap as SSO user to return a Forbidden error, actual: %v", err)
			})
		})
	})

	t.Run("space is not flagged as community", func(t *testing.T) {
		sp := toolchainv1alpha1.Space{}
		err := hostAwait.Client.Get(context.TODO(), client.ObjectKeyFromObject(space), &sp)
		require.NoError(t, err)

		/*
		   Given Space exists for user A
		   Given User community-user exists
		   Given A space's visibility is NOT "community"
		   Then  community-user cannot view A's Space
		   And   community-user cannot create resources in A's Space
		*/
		for s, c := range tt {
			t.Run(s, func(t *testing.T) {
				communityUser := createAppStudioCommunityUser(t, awaitilities, memberAwait, c.communityUserOpts...)

				t.Run("community user cannot access to non-community space", func(t *testing.T) {
					require.NotEmpty(t, sp.Status.ProvisionedNamespaces)

					proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(sp.Name)
					communityUserProxyClient, err := hostAwait.CreateAPIProxyClient(t, communityUser.Token, proxyWorkspaceURL)
					require.NoError(t, err)

					t.Run("community user cannot list config maps from community space", func(t *testing.T) {
						// then
						cms := corev1.ConfigMapList{}

						err = communityUserProxyClient.List(context.TODO(), &cms, client.InNamespace(sp.Status.ProvisionedNamespaces[0].Name))
						require.True(t, meta.IsNoMatchError(err), "expected List ConfigMap as community user to return a NoMatch error, actual: %v", err)
					})

					t.Run("community user cannot create config maps into space", func(t *testing.T) {
						cm := corev1.ConfigMap{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-cm",
								Namespace: sp.Status.ProvisionedNamespaces[0].Name,
							},
						}
						err := communityUserProxyClient.Create(context.TODO(), &cm)
						require.True(t, meta.IsNoMatchError(err), "expected Create ConfigMap as community user to return a NoMatch error, actual: %v", err)
					})
				})
			})
		}
	})
}

func createAppStudioCommunityUser(t *testing.T, awaitilities wait.Awaitilities, memberAwait *wait.MemberAwaitility, withOptions ...func(*SignupRequest) *SignupRequest) *SignupResult {
	user := &proxyUser{
		expectedMemberCluster: memberAwait,
		username:              "community-user",
		identityID:            uuid.New(),
	}

	// Create and approve signup
	sr := NewSignupRequest(awaitilities).
		Username(user.username).
		IdentityID(user.identityID).
		Email("communityuser@teste2e.com").
		SpaceTier("appstudio").
		RequireConditions(wait.Default()...)
	for _, opts := range withOptions {
		sr = opts(sr)
	}
	result := sr.Execute(t)

	user.signup = result.UserSignup
	user.token = result.Token
	user.compliantUsername = user.signup.Status.CompliantUsername

	return result
}

func CreateCommunitySpaceBinding(
	t *testing.T,
	hostAwait *wait.HostAwaitility,
	spaceName, spaceNamespace string,
) *toolchainv1alpha1.SpaceBinding {
	return spacebinding.CreateSpaceBindingStr(t, hostAwait, toolchainv1alpha1.KubesawAuthenticatedUsername, spaceName, spaceNamespace, "viewer")
}
