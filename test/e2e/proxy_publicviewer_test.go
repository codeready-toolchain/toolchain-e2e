package e2e_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
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
	signupResult *SignupResult
	email        string
	token        string
}

func (u *proxyUser) Token() string {
	switch {
	case u.token != "":
		return u.token
	case u.signupResult != nil:
		return u.signupResult.Token
	default:
		return ""
	}
}

func (u *proxyUser) Email() string {
	switch {
	case u.email != "":
		return u.email
	case u.signupResult != nil:
		return u.signupResult.UserSignup.Spec.IdentityClaims.Email
	default:
		return ""
	}
}

// tests access to community-shared spaces
func TestProxyPublicViewer(t *testing.T) {
	// make sure everything is ready before running the actual tests
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()

	// public viewer is enabled in ToolchainConfig
	hostAwait.UpdateToolchainConfig(t, testconfig.PublicViewerConfig(true))

	// we create a space to share
	space, _, _ := testsupportspace.CreateSpace(t, awaitilities,
		testspace.WithTierName("appstudio"),
		testspace.WithSpecTargetCluster(memberAwait.ClusterName),
	)

	// users test cases
	tt := map[string]struct {
		proxyClientUser func() proxyUser
	}{
		"approved user with space": {
			proxyClientUser: func() proxyUser {
				user := createAppStudioRandomUser(t, awaitilities, func(sr *SignupRequest) *SignupRequest {
					return sr.
						ManuallyApprove().
						EnsureMUR().
						RequireConditions(
							wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...)
				})
				return proxyUser{signupResult: user}
			},
		},
		"approved user without space": {
			proxyClientUser: func() proxyUser {
				user := createAppStudioRandomUser(t, awaitilities, func(sr *SignupRequest) *SignupRequest {
					return sr.
						ManuallyApprove().
						EnsureMUR().
						NoSpace().
						RequireConditions(
							wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...)
				})
				return proxyUser{signupResult: user}
			},
		},
		"not approved user": {
			proxyClientUser: func() proxyUser {
				user := createAppStudioRandomUser(t, awaitilities, func(sr *SignupRequest) *SignupRequest {
					return sr.
						NoSpace().
						RequireConditions(
							wait.ConditionSet(wait.Default(), wait.VerificationRequired())...)
				})
				return proxyUser{signupResult: user}
			},
		},
		"sso user": {
			proxyClientUser: func() proxyUser {
				userIdentity := &commonauth.Identity{
					ID:       uuid.New(),
					Username: "joe",
				}
				email := "joe@joe.joe"
				claims := []commonauth.ExtraClaim{commonauth.WithEmailClaim(email)}
				token, err := authsupport.NewTokenFromIdentity(userIdentity, claims...)
				require.NoError(t, err)
				return proxyUser{token: token, email: email}
			},
		},
	}

	t.Run("space is flagged as community", func(t *testing.T) {
		sb := CreateCommunitySpaceBinding(t, hostAwait, space.Name, space.Namespace)
		require.NotNil(t, sb)

		// Wait until space is flagged as community
		_, err := hostAwait.WaitForSpaceBinding(t, toolchainv1alpha1.KubesawAuthenticatedUsername, space.Name, wait.UntilSpaceBindingHasSpaceRole("viewer"))
		require.NoError(t, err)

		// wait until the space has ProvisionedNamespaces
		sp, err := hostAwait.WaitForSpace(t, space.Name, wait.UntilSpaceHasAnyProvisionedNamespaces())
		require.NoError(t, err)

		t.Run("user is not banned", func(t *testing.T) {
			for s, c := range tt {
				t.Run(s, func(t *testing.T) {
					// build proxy client
					user := c.proxyClientUser()
					proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(sp.Name)
					communityUserProxyClient, err := hostAwait.CreateAPIProxyClient(t, user.Token(), proxyWorkspaceURL)
					require.NoError(t, err)

					t.Run("can list config maps", func(t *testing.T) {
						cms := corev1.ConfigMapList{}
						err = communityUserProxyClient.List(context.TODO(), &cms, client.InNamespace(sp.Status.ProvisionedNamespaces[0].Name))
						require.NoError(t, err)
					})

					t.Run("cannot create config maps", func(t *testing.T) {
						cm := corev1.ConfigMap{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "test-cm",
								Namespace: sp.Status.ProvisionedNamespaces[0].Name,
							},
						}
						err = communityUserProxyClient.Create(context.TODO(), &cm)
						require.ErrorContains(t, err, "configmaps is forbidden: User \"kubesaw-authenticated\" cannot create resource \"configmaps\" in API group")
					})
				})
			}
		})

		t.Run("user is banned", func(t *testing.T) {
			for s, c := range tt {
				t.Run(s, func(t *testing.T) {
					user := c.proxyClientUser()
					// the client needs to be created before the ban,
					// otherwise it won't initialize properly
					url := hostAwait.ProxyURLWithWorkspaceContext(sp.Name)
					proxyClient, err := hostAwait.CreateAPIProxyClient(t, user.Token(), url)
					require.NoError(t, err)

					banUser(t, hostAwait, user)

					t.Run(s, func(t *testing.T) {
						t.Run("user cannot initialize a new client", func(t *testing.T) {
							url := hostAwait.ProxyURLWithWorkspaceContext(sp.Name)
							proxyClient, err := hostAwait.CreateAPIProxyClient(t, user.Token(), url)
							require.NoError(t, err)

							// as the client is not initialized correctly,
							// any request should return a NoMatch error
							cm := corev1.ConfigMap{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "test-cm",
									Namespace: sp.Status.ProvisionedNamespaces[0].Name,
								},
							}
							err = proxyClient.Create(context.TODO(), &cm)
							require.ErrorContains(t, err, "user access is forbidden")
						})

						t.Run("cannot list config maps", func(t *testing.T) {
							cms := corev1.ConfigMapList{}
							err := proxyClient.List(context.TODO(), &cms, client.InNamespace(sp.Status.ProvisionedNamespaces[0].Name))
							require.Zero(t, cms)
							require.Error(t, err)
						})

						t.Run("cannot create config maps", func(t *testing.T) {
							cm := corev1.ConfigMap{
								ObjectMeta: metav1.ObjectMeta{
									Name:      "test-cm",
									Namespace: sp.Status.ProvisionedNamespaces[0].Name,
								},
							}
							err := proxyClient.Create(context.TODO(), &cm)
							require.ErrorContains(t, err, "user access is forbidden")
						})
					})
				})
			}
		})
	})

	t.Run("space is not flagged as community", func(t *testing.T) {
		// retrieve the space
		sp := toolchainv1alpha1.Space{}
		err := hostAwait.Client.Get(context.TODO(), client.ObjectKeyFromObject(space), &sp)
		require.NoError(t, err)

		// ensure no SpaceBinding exists for Public-Viewer
		sbs, err := hostAwait.GetSpaceBindingByListing(toolchainv1alpha1.KubesawAuthenticatedUsername, space.Name)
		require.NoError(t, err)
		require.Empty(t, sbs)

		testCases := map[string]struct {
			banned        bool
			expectedError string
		}{
			"user is not banned": {
				banned:        false,
				expectedError: "invalid workspace request",
			},
			"user is banned": {
				banned:        true,
				expectedError: "user access is forbidden",
			},
		}
		for str, testCase := range testCases {
			t.Run(str, func(t *testing.T) {
				for s, c := range tt {
					t.Run(s, func(t *testing.T) {
						user := c.proxyClientUser()
						if testCase.banned {
							banUser(t, hostAwait, user)
						}

						t.Run("user cannot access to non-community space", func(t *testing.T) {
							require.NotEmpty(t, sp.Status.ProvisionedNamespaces)

							proxyWorkspaceURL := hostAwait.ProxyURLWithWorkspaceContext(sp.Name)
							communityUserProxyClient, err := hostAwait.CreateAPIProxyClient(t, user.Token(), proxyWorkspaceURL)
							require.NoError(t, err)

							t.Run("user cannot list config maps from non-community space", func(t *testing.T) {
								cms := corev1.ConfigMapList{}

								err = communityUserProxyClient.List(context.TODO(), &cms, client.InNamespace(sp.Status.ProvisionedNamespaces[0].Name))
								require.ErrorContains(t, err, testCase.expectedError)
							})

							t.Run("user cannot create config maps into non-community space", func(t *testing.T) {
								cm := corev1.ConfigMap{
									ObjectMeta: metav1.ObjectMeta{
										Name:      "test-cm",
										Namespace: sp.Status.ProvisionedNamespaces[0].Name,
									},
								}
								err := communityUserProxyClient.Create(context.TODO(), &cm)
								require.ErrorContains(t, err, testCase.expectedError)
							})
						})
					})
				}
			})
		}
	})
}

func banUser(t *testing.T, hostAwait *wait.HostAwaitility, user proxyUser) {
	bannedUser := NewBannedUser(hostAwait, user.Email())
	err := hostAwait.CreateWithCleanup(t, bannedUser)
	require.NoError(t, err)

	if user.signupResult != nil {
		_, err = hostAwait.
			WithRetryOptions(wait.TimeoutOption(time.Second*10), wait.RetryInterval(time.Second*2)).
			WaitForUserSignup(t, user.signupResult.UserSignup.Name, wait.UntilUserSignupHasConditions(
				wait.ConditionSet(user.signupResult.UserSignup.Status.Conditions, wait.Banned())...))
		require.NoError(t, err)
	}
}

func createAppStudioRandomUser(t *testing.T, awaitilities wait.Awaitilities, withOptions ...func(*SignupRequest) *SignupRequest) *SignupResult {
	suffix := rand.Int31n(999999) // nolint:gosec
	sr := NewSignupRequest(awaitilities).
		Username(fmt.Sprintf("user-%d", suffix)).
		IdentityID(uuid.New()).
		Email(fmt.Sprintf("user-%d@teste2e.com", suffix)).
		SpaceTier("appstudio").
		RequireConditions(wait.Default()...)
	for _, opts := range withOptions {
		sr = opts(sr)
	}
	return sr.Execute(t)
}

func CreateCommunitySpaceBinding(
	t *testing.T,
	hostAwait *wait.HostAwaitility,
	spaceName, spaceNamespace string,
) *toolchainv1alpha1.SpaceBinding {
	return spacebinding.CreateSpaceBindingStr(t, hostAwait, toolchainv1alpha1.KubesawAuthenticatedUsername, spaceName, spaceNamespace, "viewer")
}
