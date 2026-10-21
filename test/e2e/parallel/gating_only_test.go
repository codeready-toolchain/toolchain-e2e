package parallel

import (
	"context"
	"net/http"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRequestsWithGatingOnly(t *testing.T) {
	// given
	t.Parallel()
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	route := hostAwait.RegistrationServiceURL
	gatingSignupPath := route + "/api/v1/signup?gating-only=true"

	// when
	// request with gating only
	user := NewSignupRequest(awaitilities).
		Email("gating@redhat.com").
		GatingOnly().
		RequireConditions(wait.NoProvisioningConditions()...).
		Execute(t)
	userSignupName := user.UserSignup.Name

	// then
	// created UserSignup with no-provisioning state
	userSignup, err := hostAwait.WaitForUserSignup(t, userSignupName,
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNoProvisioning),
		wait.UntilUserSignupMatchesStates(toolchainv1alpha1.UserSignupStateNoProvisioning),
		wait.UntilUserSignupHasAnnotationNotEmpty(toolchainv1alpha1.UserSignupVerifiedTimestampAnnotationKey))
	require.NoError(t, err)
	// activation counter is not set
	assert.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupActivationCounterAnnotationKey])
	// no compliant username set, and gating has not been requested yet
	require.Empty(t, userSignup.Status.CompliantUsername)
	assert.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupGatingOnlyKey])
	noMurSpaceCreated(t, hostAwait, userSignupName)
	var gatingOnlyAt string

	t.Run("Get request with gating-only returns verified", func(t *testing.T) {
		// when
		_, signupStatus := ParseSignupResponse(t,
			NewHTTPRequest(t).
				InvokeEndpoint("GET", gatingSignupPath, user.Token, "", http.StatusOK).
				UnmarshalMap())

		// then
		require.False(t, signupStatus["verificationRequired"].(bool))
		require.True(t, signupStatus["verified"].(bool))
		userSignup, err = hostAwait.WaitForUserSignup(t, userSignupName,
			wait.UntilUserSignupHasAnnotationNotEmpty(toolchainv1alpha1.UserSignupGatingOnlyKey))
		require.NoError(t, err)
		gatingOnlyAt = userSignup.Annotations[toolchainv1alpha1.UserSignupGatingOnlyKey]

		t.Run("another gating-only GET leaves the annotation", func(t *testing.T) {
			// when
			_, signupStatus := ParseSignupResponse(t,
				NewHTTPRequest(t).
					InvokeEndpoint("GET", gatingSignupPath, user.Token, "", http.StatusOK).
					UnmarshalMap())

			// then
			require.False(t, signupStatus["verificationRequired"].(bool))
			require.True(t, signupStatus["verified"].(bool))
			_, err = hostAwait.WaitForUserSignup(t, userSignupName,
				wait.UntilUserSignupHasAnnotation(toolchainv1alpha1.UserSignupGatingOnlyKey, gatingOnlyAt))
			require.NoError(t, err)
		})
	})

	t.Run("Get request without gating-only returns not-found", func(t *testing.T) {
		// when
		NewHTTPRequest(t).InvokeEndpoint("GET", route+"/api/v1/signup", user.Token, "", http.StatusNotFound)

		// then
		_, err = hostAwait.WaitForUserSignup(t, userSignupName,
			wait.UntilUserSignupHasAnnotation(toolchainv1alpha1.UserSignupGatingOnlyKey, gatingOnlyAt))
		require.NoError(t, err)
	})

	t.Run("another POST request with gating-only fails with conflict", func(t *testing.T) {
		// when & then
		NewHTTPRequest(t).InvokeEndpoint("POST", gatingSignupPath, user.Token, "", http.StatusConflict)
	})

	t.Run("when verification expires then GET request with gating-only returns not found", func(t *testing.T) {
		// given
		_, err := wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
			Update(userSignupName, hostAwait.Namespace, func(us *toolchainv1alpha1.UserSignup) {
				us.Annotations[toolchainv1alpha1.UserSignupVerifiedTimestampAnnotationKey] = time.Now().Add(-10 * 24 * time.Hour).Format(time.RFC3339)
				// let's remove it to verify that the GET won't add it back when it's expired (returns not found requested reactivation)
				delete(us.Annotations, toolchainv1alpha1.UserSignupGatingOnlyKey)
			})
		require.NoError(t, err)

		// when
		NewHTTPRequest(t).
			InvokeEndpoint("GET", gatingSignupPath, user.Token, "", http.StatusNotFound)

		// then — expired verification returns before the annotation is written
		userSignup, err = hostAwait.WaitForUserSignup(t, userSignupName)
		require.NoError(t, err)
		assert.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupGatingOnlyKey])

		t.Run("and it is possible to reactivate", func(t *testing.T) {
			// when
			NewHTTPRequest(t).InvokeEndpoint("POST", gatingSignupPath, user.Token, "", http.StatusAccepted)

			// then
			userSignup, err := hostAwait.WaitForUserSignup(t, userSignupName,
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNoProvisioning),
				wait.UntilUserSignupMatchesStates(toolchainv1alpha1.UserSignupStateNoProvisioning),
				wait.UntilUserSignupHasAnnotationNotEmpty(toolchainv1alpha1.UserSignupVerifiedTimestampAnnotationKey))
			require.NoError(t, err)
			// activation counter is not set, and reactivation does not add gating-only timestamp
			assert.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupActivationCounterAnnotationKey])
			assert.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupGatingOnlyKey])
		})
	})

	t.Run("reactivate as normal signup", func(t *testing.T) {
		// given
		existingSignup, err := hostAwait.WaitForUserSignup(t, userSignupName)
		require.NoError(t, err)

		// when
		NewHTTPRequest(t).InvokeEndpoint("POST", route+"/api/v1/signup", user.Token, "", http.StatusAccepted)

		// then
		_, err = hostAwait.WaitForUserSignup(t, userSignupName,
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValuePending),
			wait.UntilUserSignupMatchesStates(),
			// verified timestamp annotation is preserved
			wait.UntilUserSignupHasAnnotation(toolchainv1alpha1.UserSignupVerifiedTimestampAnnotationKey, existingSignup.Annotations[toolchainv1alpha1.UserSignupVerifiedTimestampAnnotationKey]))
		require.NoError(t, err)

		// and when the signup is manually approved
		userSignup, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
			Update(userSignupName, hostAwait.Namespace, func(us *toolchainv1alpha1.UserSignup) {
				states.SetApprovedManually(us, true)
			})
		require.NoError(t, err)

		// then it's provisioned with first activation counter set
		VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup)
		_, err = hostAwait.WaitForUserSignup(t, userSignupName,
			wait.UntilUserSignupHasAnnotation(toolchainv1alpha1.UserSignupActivationCounterAnnotationKey, "1"))
		require.NoError(t, err)

		checkGetAndPost := func(t *testing.T) {
			t.Run("Get request with gating only returns verified", func(t *testing.T) {
				// when
				_, signupStatus := ParseSignupResponse(t,
					NewHTTPRequest(t).
						InvokeEndpoint("GET", gatingSignupPath, user.Token, "", http.StatusOK).
						UnmarshalMap())

				// then
				require.False(t, signupStatus["verificationRequired"].(bool))
				require.True(t, signupStatus["verified"].(bool))
				_, err = hostAwait.WaitForUserSignup(t, userSignupName,
					wait.UntilUserSignupHasAnnotationNotEmpty(toolchainv1alpha1.UserSignupGatingOnlyKey))
				require.NoError(t, err)

				t.Run("POST request fails with conflict", func(t *testing.T) {
					// when & then
					NewHTTPRequest(t).InvokeEndpoint("POST", gatingSignupPath, user.Token, "", http.StatusConflict)
				})
			})
		}
		checkGetAndPost(t)

		t.Run("when verified annotation had expired", func(t *testing.T) {
			// given
			_, err := wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
				Update(userSignupName, hostAwait.Namespace, func(us *toolchainv1alpha1.UserSignup) {
					us.Annotations[toolchainv1alpha1.UserSignupVerifiedTimestampAnnotationKey] = time.Now().Add(-10 * 24 * time.Hour).Format(time.RFC3339)
				})
			require.NoError(t, err)

			checkGetAndPost(t)
		})

		t.Run("after deactivating, we can reactivate with gating only", func(t *testing.T) {
			// given
			_, err := wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
				Update(userSignup.Name, hostAwait.Namespace,
					func(us *toolchainv1alpha1.UserSignup) {
						states.SetDeactivated(us, true)
						delete(us.Annotations, toolchainv1alpha1.UserSignupGatingOnlyKey)
					})
			require.NoError(t, err)
			_, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.DeactivatedWithoutPreDeactivation())...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
			require.NoError(t, err)

			// when — deactivated signups return before the annotation is written
			NewHTTPRequest(t).InvokeEndpoint("GET", gatingSignupPath, user.Token, "", http.StatusNotFound)

			// then
			deactivated, err := hostAwait.WaitForUserSignup(t, userSignup.Name)
			require.NoError(t, err)
			assert.Empty(t, deactivated.Annotations[toolchainv1alpha1.UserSignupGatingOnlyKey])

			// when
			NewHTTPRequest(t).InvokeEndpoint("POST", gatingSignupPath, user.Token, "", http.StatusAccepted)

			// then
			_, err = hostAwait.WaitForUserSignup(t, userSignupName,
				wait.UntilUserSignupHasConditions(wait.NoProvisioningConditions()...),
				wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNoProvisioning),
				wait.UntilUserSignupMatchesStates(toolchainv1alpha1.UserSignupStateNoProvisioning),
				wait.UntilUserSignupHasAnnotationNotEmpty(toolchainv1alpha1.UserSignupVerifiedTimestampAnnotationKey),
				// activation counter stays the same
				wait.UntilUserSignupHasAnnotation(toolchainv1alpha1.UserSignupActivationCounterAnnotationKey, "1"),
				// compliant username stays there too
				wait.UntilUserSignupHasCompliantUsername())
			require.NoError(t, err)
			reactivated, err := hostAwait.WaitForUserSignup(t, userSignupName)
			require.NoError(t, err)
			assert.Empty(t, reactivated.Annotations[toolchainv1alpha1.UserSignupGatingOnlyKey])

			t.Run("Get request with gating only returns verified", func(t *testing.T) {
				// when
				_, signupStatus := ParseSignupResponse(t,
					NewHTTPRequest(t).
						InvokeEndpoint("GET", gatingSignupPath, user.Token, "", http.StatusOK).
						UnmarshalMap())

				// then
				require.False(t, signupStatus["verificationRequired"].(bool))
				require.True(t, signupStatus["verified"].(bool))
				_, err = hostAwait.WaitForUserSignup(t, userSignupName,
					wait.UntilUserSignupHasAnnotationNotEmpty(toolchainv1alpha1.UserSignupGatingOnlyKey))
				require.NoError(t, err)
			})
		})
	})
}

func noMurSpaceCreated(t *testing.T, hostAwait *wait.HostAwaitility, userSignupName string) {
	murs := &toolchainv1alpha1.MasterUserRecordList{}
	err := hostAwait.Client.List(context.TODO(), murs, client.MatchingLabels{toolchainv1alpha1.OwnerLabelKey: userSignupName})
	require.NoError(t, err)
	require.Empty(t, murs.Items)

	spaces := &toolchainv1alpha1.SpaceList{}
	err = hostAwait.Client.List(context.TODO(), spaces, client.MatchingLabels{toolchainv1alpha1.SpaceCreatorLabelKey: userSignupName})
	require.NoError(t, err)
	require.Empty(t, spaces.Items)

}
