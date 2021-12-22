package e2e

import (
	"context"
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/gofrs/uuid"
	userv1 "github.com/openshift/api/user/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type proxyUsers struct {
	expectedMemberCluster *wait.MemberAwaitility
	username              string
	token                 string
	identityID            uuid.UUID
	signup                *toolchainv1alpha1.UserSignup
}

// full flow from usersignup with approval down to namespaces creation and cleanup
func TestProxyFlow(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()
	hostAwait.UpdateToolchainConfig(config.Tiers().DefaultTier("appstudio"))

	// check that the tier exists, and all its namespace other cluster-scoped resource revisions
	// are different from `000000a` which is the value specified in the initial manifest (used for base tier)
	WaitUntilBaseNSTemplateTierIsUpdated(t, hostAwait)

	users := []proxyUsers{
		{
			expectedMemberCluster: memberAwait,
			username:              "proxymember1",
			identityID:            uuid.Must(uuid.NewV4()),
		},
		{
			expectedMemberCluster: memberAwait2,
			username:              "proxymember2",
			identityID:            uuid.Must(uuid.NewV4()),
		},
	}

	// if there is an identity & user resources already present, but don't contain "owner" label, then they shouldn't be deleted
	preexistingUser, preexistingIdentity := createPreexistingUserAndIdentity(t, users[0])

	for index, user := range users {
		t.Run(user.username, func(t *testing.T) {
			// Create and approve signup
			req := NewSignupRequest(t, awaitilities).
				Username(user.username).
				IdentityID(user.identityID).
				ManuallyApprove().
				TargetCluster(user.expectedMemberCluster).
				EnsureMUR().
				RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
				Execute()

			user.signup, _ = req.Resources()
			user.token = req.GetToken()

			VerifyResourcesProvisionedForSignup(t, awaitilities, user.signup, "appstudio")
			_, err := hostAwait.GetMasterUserRecord(wait.WithMurName(user.username))
			require.NoError(t, err)

			t.Run("use proxy to create a configmap in the user appstudio namespace via proxy API", func(t *testing.T) {
				// given
				cmName := fmt.Sprintf("%s-test-cm", user.username)
				expectedCM := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cmName,
						Namespace: user.username,
					},
					Data: map[string]string{
						"planet": "venus",
					},
				}

				// when
				proxyCl := hostAwait.CreateAPIProxyClient(user.token)
				err := proxyCl.Create(context.TODO(), expectedCM)
				require.NoError(t, err)

				// then
				createdCM := &corev1.ConfigMap{}
				err = user.expectedMemberCluster.Client.Get(context.TODO(), types.NamespacedName{Namespace: user.username, Name: cmName}, createdCM)
				require.NoError(t, err)
				require.NotEmpty(t, createdCM)
				require.Equal(t, "venus", createdCM.Data["planet"])
			})

			t.Run("try to create a resource in an unauthorized namespace", func(t *testing.T) {
				// given
				cmName := fmt.Sprintf("%s-proxy-test-cm", user.username)
				expectedCM := &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      cmName,
						Namespace: hostAwait.Namespace, // user should not be allowed to create a resource in the host operator namespace
					},
					Data: map[string]string{
						"planet": "venus",
					},
				}

				// when
				proxyCl := hostAwait.CreateAPIProxyClient(user.token)

				// then
				err := proxyCl.Create(context.TODO(), expectedCM)
				require.EqualError(t, err, fmt.Sprintf(`configmaps is forbidden: User "system:serviceaccount:%[1]s:appstudio-%[1]s" cannot create resource "configmaps" in API group "" in the namespace "%[2]s"`, user.username, hostAwait.Namespace))
			})

			if index == 1 { // only for the second user
				t.Run("try to create a resource in the other users namespace", func(t *testing.T) {
					// given
					// verify first user's namespace still exists
					ns := &corev1.Namespace{}
					err := hostAwait.Client.Get(context.TODO(), types.NamespacedName{Name: users[0].username}, ns)
					require.NoError(t, err, "failed to verify the first user's namespace still exists")

					cmName := fmt.Sprintf("%s-proxy-test-cm", users[0].username)
					expectedCM := &corev1.ConfigMap{
						ObjectMeta: metav1.ObjectMeta{
							Name:      cmName,
							Namespace: users[0].expectedMemberCluster.Namespace, // user should not be allowed to create a resource in the first user's namespace
						},
						Data: map[string]string{
							"planet": "venus",
						},
					}

					// when
					proxyCl := hostAwait.CreateAPIProxyClient(user.token)
					err = proxyCl.Create(context.TODO(), expectedCM)

					// then
					require.EqualError(t, err, fmt.Sprintf(`configmaps is forbidden: User "system:serviceaccount:%[1]s:appstudio-%[1]s" cannot create resource "configmaps" in API group "" in the namespace "%[2]s"`, user.username, users[0].expectedMemberCluster.Namespace))
				})
			}
		})
	} // end users loop

	// preexisting user & identity are still there
	// Verify provisioned User
	_, err := memberAwait.WaitForUser(preexistingUser.Name)
	assert.NoError(t, err)

	// Verify provisioned Identity
	_, err = memberAwait.WaitForIdentity(preexistingIdentity.Name)
	assert.NoError(t, err)
}

func createPreexistingUserAndIdentity(t *testing.T, user proxyUsers) (*userv1.User, *userv1.Identity) {
	preexistingUser := &userv1.User{
		ObjectMeta: metav1.ObjectMeta{
			Name: user.username,
		},
		Identities: []string{
			ToIdentityName(user.identityID.String()),
		},
	}
	require.NoError(t, user.expectedMemberCluster.CreateWithCleanup(context.TODO(), preexistingUser))

	preexistingIdentity := &userv1.Identity{
		ObjectMeta: metav1.ObjectMeta{
			Name: ToIdentityName(user.identityID.String()),
		},
		ProviderName:     "rhd",
		ProviderUserName: user.username,
		User: corev1.ObjectReference{
			Name: preexistingUser.Name,
			UID:  preexistingUser.UID,
		},
	}
	require.NoError(t, user.expectedMemberCluster.CreateWithCleanup(context.TODO(), preexistingIdentity))
	return preexistingUser, preexistingIdentity
}
