package parallel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	commonsocialevent "github.com/codeready-toolchain/toolchain-common/pkg/socialevent"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	commonauth "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	testsocialevent "github.com/codeready-toolchain/toolchain-common/pkg/test/socialevent"
	commonsignup "github.com/codeready-toolchain/toolchain-common/pkg/usersignup"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	authsupport "github.com/codeready-toolchain/toolchain-e2e/testsupport/auth"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/cleanup"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/davecgh/go-spew/spew"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
)

var httpClient = HTTPClient

func TestLandingPageReachable(t *testing.T) {
	// given
	t.Parallel()
	await := WaitForDeployments(t)
	route := await.Host().RegistrationServiceURL

	// just make sure that the landing page is reachable
	req, err := http.NewRequest("GET", route, nil)
	require.NoError(t, err)

	resp, err := httpClient.Do(req) // nolint:bodyclose // see `defer Close(t, resp)`
	require.NoError(t, err)
	defer Close(t, resp)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHealth(t *testing.T) {
	// given
	t.Parallel()
	await := WaitForDeployments(t)
	route := await.Host().RegistrationServiceURL

	t.Run("get healthcheck 200 OK", func(t *testing.T) {
		// Call health endpoint.
		req, err := http.NewRequest("GET", route+"/api/v1/health", nil)
		require.NoError(t, err)

		resp, err := httpClient.Do(req) //nolint:bodyclose // see `defer Close(...)`
		require.NoError(t, err)
		defer Close(t, resp)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NotNil(t, body)

		mp := make(map[string]interface{})
		err = json.Unmarshal([]byte(body), &mp)
		require.NoError(t, err)

		// Verify JSON response.
		alive := mp["alive"]
		require.IsType(t, true, alive)
		require.True(t, alive.(bool))

		environment := mp["environment"]
		require.IsType(t, "", environment)
		require.Equal(t, "e2e-tests", environment.(string))

		revision := mp["revision"]
		require.NotNil(t, revision)

		buildTime := mp["buildTime"]
		require.NotNil(t, buildTime)

		startTime := mp["startTime"]
		require.NotNil(t, startTime)
	})
}

func TestAnalytics(t *testing.T) {
	// given
	t.Parallel()
	await := WaitForDeployments(t)
	route := await.Host().RegistrationServiceURL

	assertNotSecuredGetResponseEquals := func(endPointPath, expectedResponseValue string) {
		// Call analytics domain endpoint.
		req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/%s", route, endPointPath), nil)
		require.NoError(t, err)

		resp, err := httpClient.Do(req) //nolint:bodyclose // see `defer Close(...)`
		require.NoError(t, err)
		defer Close(t, resp)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NotNil(t, body)

		value := string(body)
		require.NoError(t, err)

		// Verify JSON response.
		require.Equal(t, expectedResponseValue, value)
	}

	t.Run("get devspaces segment write key 200 OK", func(t *testing.T) {
		// Call segment write key endpoint.
		assertNotSecuredGetResponseEquals("segment-write-key", "test devspaces segment write key")
	})
}

func TestAuthConfig(t *testing.T) {
	// given
	t.Parallel()
	await := WaitForDeployments(t)
	route := await.Host().RegistrationServiceURL

	t.Run("get authconfig 200 OK", func(t *testing.T) {
		// Call authconfig endpoint.
		req, err := http.NewRequest("GET", route+"/api/v1/authconfig", nil)
		require.NoError(t, err)

		resp, err := httpClient.Do(req) //nolint:bodyclose // see `defer Close(...)`
		require.NoError(t, err)
		defer Close(t, resp)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NotNil(t, body)
	})
}

func TestSignupFails(t *testing.T) {
	// given
	t.Parallel()
	await := WaitForDeployments(t)
	route := await.Host().RegistrationServiceURL

	t.Run("post signup error no token 401 Unauthorized", func(t *testing.T) {
		// Call signup endpoint without a token.
		requestBody, err := json.Marshal(map[string]string{})
		require.NoError(t, err)
		req, err := http.NewRequest("POST", route+"/api/v1/signup", bytes.NewBuffer(requestBody))
		require.NoError(t, err)
		req.Header.Set("content-type", "application/json")

		resp, err := httpClient.Do(req) // nolint:bodyclose // see `defer.Close(...)`
		defer Close(t, resp)
		require.NoError(t, err)

		// Retrieve unauthorized http status code.
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NotNil(t, body)

		mp := make(map[string]interface{})
		err = json.Unmarshal([]byte(body), &mp)
		require.NoError(t, err)

		// Check token error.
		tokenErr := mp["error"]
		require.Equal(t, "no token found", tokenErr.(string))
	})
	t.Run("post signup error invalid token 401 Unauthorized", func(t *testing.T) {
		// Call signup endpoint with an invalid token.
		mp := InvokeEndpoint(t, "POST", route+"/api/v1/signup", "1223123123", "", http.StatusUnauthorized)

		// Check token error.
		tokenErr := mp["error"]
		require.Equal(t, "token contains an invalid number of segments", tokenErr.(string))
	})
	t.Run("post signup exp token 401 Unauthorized", func(t *testing.T) {
		emailAddress := uuid.Must(uuid.NewV4()).String() + "@acme.com"
		// Not identical to the token used in POST signup - should return resource not found.
		_, token1, err := authsupport.NewToken(
			authsupport.WithEmail(emailAddress),
			authsupport.WithExp(time.Now().Add(-60*time.Second)))
		require.NoError(t, err)
		mp := InvokeEndpoint(t, "POST", route+"/api/v1/signup", token1, "", http.StatusUnauthorized)

		// Check token error.
		tokenErr := mp["error"]
		require.Contains(t, tokenErr.(string), "token is expired by ")
	})
	t.Run("get signup error no token 401 Unauthorized", func(t *testing.T) {
		// Call signup endpoint without a token.
		req, err := http.NewRequest("GET", route+"/api/v1/signup", nil)
		require.NoError(t, err)
		req.Header.Set("content-type", "application/json")

		resp, err := httpClient.Do(req) // nolint:bodyclose // see `defer.Close(...)`
		require.NoError(t, err)
		defer Close(t, resp)

		// Retrieve unauthorized http status code.
		assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NotNil(t, body)

		mp := make(map[string]interface{})
		err = json.Unmarshal([]byte(body), &mp)
		require.NoError(t, err)

		// Check token error.
		tokenErr := mp["error"]
		require.Equal(t, "no token found", tokenErr.(string))
	})
	t.Run("get signup error invalid token 401 Unauthorized", func(t *testing.T) {
		// Call signup endpoint with an invalid token.
		mp := InvokeEndpoint(t, "GET", route+"/api/v1/signup", "1223123123", "", http.StatusUnauthorized)

		// Check token error.
		tokenErr := mp["error"]
		require.Equal(t, "token contains an invalid number of segments", tokenErr.(string))
	})
	t.Run("get signup exp token 401 Unauthorized", func(t *testing.T) {
		emailAddress := uuid.Must(uuid.NewV4()).String() + "@acme.com"
		// Not identical to the token used in POST signup - should return resource not found.
		_, token1, err := authsupport.NewToken(
			authsupport.WithEmail(emailAddress),
			authsupport.WithExp(time.Now().Add(-60*time.Second)))
		require.NoError(t, err)
		mp := InvokeEndpoint(t, "GET", route+"/api/v1/signup", token1, "", http.StatusUnauthorized)

		// Check token error.
		tokenErr := mp["error"]
		require.Contains(t, tokenErr.(string), "token is expired by ")
	})
	t.Run("get signup 404 NotFound", func(t *testing.T) {
		// Get valid generated token for e2e tests. IAT claim is overridden
		// to avoid token used before issued error.
		// Not identical to the token used in POST signup - should return resource not found.
		_, token1, err := authsupport.NewToken(
			authsupport.WithEmail(uuid.Must(uuid.NewV4()).String()+"@acme.com"),
			authsupport.WithIAT(time.Now().Add(-60*time.Second)))

		require.NoError(t, err)

		// Call signup endpoint with a valid token.
		assertGetSignupReturnsNotFound(t, await, token1)
	})

	t.Run("get signup for crtadmin fails", func(t *testing.T) {
		// Get valid generated token for e2e tests. IAT claim is overridden
		// to avoid token used before issued error. Username claim is also
		// overridden to trigger error and ensure that usersignup is not created.
		emailAddress := uuid.Must(uuid.NewV4()).String() + "@acme.com"
		identity, token, err := authsupport.NewToken(
			authsupport.WithEmail(emailAddress),
			authsupport.WithPreferredUsername("test-crtadmin"))
		require.NoError(t, err)

		// Call signup endpoint with a valid token to initiate a signup process
		response := InvokeEndpoint(t, "POST", route+"/api/v1/signup", token, "", http.StatusForbidden)
		require.Equal(t, "forbidden: failed to create usersignup for test-crtadmin", response["message"])
		require.Equal(t, "error creating UserSignup resource", response["details"])
		require.Equal(t, float64(403), response["code"])

		hostAwait := await.Host()
		hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*15)).WaitAndVerifyThatUserSignupIsNotCreated(t, identity.ID.String())
	})

	// since transformUsername code in toolchain-common truncates the username to be not more than 20 characters, this test is to make sure that creating a crtadmin with a longer username still fails
	t.Run("get signup for crtadmin with longer username fails", func(t *testing.T) {
		// Get valid generated token for e2e tests. IAT claim is overridden
		// to avoid token used before issued error. Username claim is also
		// overridden to trigger error and ensure that usersignup is not created.
		emailAddress := uuid.Must(uuid.NewV4()).String() + "@acme.com"
		identity, token, err := authsupport.NewToken(
			authsupport.WithEmail(emailAddress),
			authsupport.WithPreferredUsername("longer-username-crtadmin")) // when username is greater than 20 characters,
		require.NoError(t, err)

		// Call signup endpoint with a valid token to initiate a signup process
		response := InvokeEndpoint(t, "POST", route+"/api/v1/signup", token, "", http.StatusForbidden)
		require.Equal(t, "forbidden: failed to create usersignup for longer-username-crtadmin", response["message"])
		require.Equal(t, "error creating UserSignup resource", response["details"])
		require.Equal(t, float64(403), response["code"])

		hostAwait := await.Host()
		hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*15)).WaitAndVerifyThatUserSignupIsNotCreated(t, identity.ID.String())
	})
}

func TestSignupOK(t *testing.T) {
	// given
	t.Parallel()
	await := WaitForDeployments(t)
	route := await.Host().RegistrationServiceURL

	hostAwait := await.Host()
	memberAwait := await.Member1()
	signupUser := func(token, email, userSignupName string, identity *commonauth.Identity) *toolchainv1alpha1.UserSignup {
		// Call signup endpoint with a valid token to initiate a signup process
		InvokeEndpoint(t, "POST", route+"/api/v1/signup", token, "", http.StatusAccepted)

		// Wait for the UserSignup to be created
		userSignup, err := hostAwait.WaitForUserSignup(t, userSignupName,
			wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.PendingApproval())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValuePending))
		require.NoError(t, err)
		cleanup.AddCleanTasks(t, hostAwait.Client, userSignup)
		emailAnnotation := userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey]
		assert.Equal(t, email, emailAnnotation)

		// Call get signup endpoint with a valid token and make sure it's pending approval
		assertGetSignupStatusPendingApproval(t, await, identity.Username, token)

		// Attempt to create same usersignup by calling post signup with same token should return an error
		mp := InvokeEndpoint(t, "POST", route+"/api/v1/signup", token, "", http.StatusConflict)
		assert.Equal(t, fmt.Sprintf("Operation cannot be fulfilled on  \"\": UserSignup [id: %s; username: %s]. Unable to create UserSignup because there is already an active UserSignup with such ID",
			identity.ID, identity.Username), mp["message"])
		assert.Equal(t, "error creating UserSignup resource", mp["details"])

		userSignup, err = hostAwait.UpdateUserSignup(t, userSignup.Name,
			func(instance *toolchainv1alpha1.UserSignup) {
				// Approve usersignup.
				states.SetApprovedManually(instance, true)
				instance.Spec.TargetCluster = memberAwait.ClusterName
			})
		require.NoError(t, err)

		// Wait for the resources to be provisioned
		VerifyResourcesProvisionedForSignup(t, await, userSignup, "deactivate30", "base")

		// Call signup endpoint with same valid token to check if status changed to Provisioned now
		assertGetSignupStatusProvisioned(t, await, identity.Username, token)

		return userSignup
	}

	t.Run("test activation-deactivation workflow", func(t *testing.T) {
		// Get valid generated token for e2e tests. IAT claim is overridden
		// to avoid token used before issued error.
		emailAddress := uuid.Must(uuid.NewV4()).String() + "@acme.com"
		identity, token, err := authsupport.NewToken(authsupport.WithEmail(emailAddress))
		require.NoError(t, err)

		// Signup a new user
		userSignup := signupUser(token, emailAddress, identity.Username, identity)

		// Deactivate the usersignup
		userSignup, err = hostAwait.UpdateUserSignup(t, userSignup.Name,
			func(us *toolchainv1alpha1.UserSignup) {
				states.SetDeactivated(us, true)
			})
		require.NoError(t, err)
		_, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin(), wait.DeactivatedWithoutPreDeactivation())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
		require.NoError(t, err)

		// Now check that the reg-service treats the deactivated usersignup as nonexistent and returns 404
		assertGetSignupReturnsNotFound(t, await, token)

		// Re-activate the usersignup by calling the signup endpoint with the same token/user again
		signupUser(token, emailAddress, identity.Username, identity)
	})
}
func TestUserSignupFoundWhenNamedWithEncodedUsername(t *testing.T) {
	// given
	t.Parallel()
	await := WaitForDeployments(t)
	route := await.Host().RegistrationServiceURL

	hostAwait := await.Host()

	// Create a token and identity to sign up with, but override the username with "arnold" so that we create a UserSignup
	// with that name
	emailAddress := "arnold@acme.com"
	_, token0, err := authsupport.NewToken(
		authsupport.WithEmail(emailAddress),
		// authsupport.WithSub(identity0.ID.String()),
		authsupport.WithPreferredUsername("arnold"))
	require.NoError(t, err)

	// Call the signup endpoint
	InvokeEndpoint(t, "POST", route+"/api/v1/signup", token0, "", http.StatusAccepted)

	// Wait for the UserSignup to be created
	userSignup, err := hostAwait.WaitForUserSignup(t, "arnold",
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.PendingApproval())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValuePending))
	require.NoError(t, err)
	cleanup.AddCleanTasks(t, hostAwait.Client, userSignup)
	emailAnnotation := userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey]
	assert.Equal(t, emailAddress, emailAnnotation)

	// Call get signup endpoint with a valid token, however we will now override the claims to introduce the original
	// sub claim and set username as a separate claim, then we will make sure the UserSignup is returned correctly
	_, token0, err = authsupport.NewToken(
		authsupport.WithEmail(emailAddress),
		authsupport.WithPreferredUsername("arnold"))
	require.NoError(t, err)
	mp, mpStatus := ParseResponse(t, InvokeEndpoint(t, "GET", route+"/api/v1/signup", token0, "", http.StatusOK))
	assert.Equal(t, "", mp["compliantUsername"])
	assert.Equal(t, "arnold", mp["username"])
	require.IsType(t, false, mpStatus["ready"])
	assert.False(t, mpStatus["ready"].(bool))
	assert.Equal(t, "PendingApproval", mpStatus["reason"])
}

func TestPhoneVerification(t *testing.T) {
	// given
	t.Parallel()
	await := WaitForDeployments(t)
	route := await.Host().RegistrationServiceURL

	hostAwait := await.Host()
	// Create a token and identity to sign up with
	emailAddress := uuid.Must(uuid.NewV4()).String() + "@some.domain"
	identity0, token0, err := authsupport.NewToken(authsupport.WithEmail(emailAddress))
	require.NoError(t, err)

	// Call the signup endpoint
	InvokeEndpoint(t, "POST", route+"/api/v1/signup", token0, "", http.StatusAccepted)

	// Wait for the UserSignup to be created
	userSignup, err := hostAwait.WaitForUserSignup(t, identity0.Username,
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(t, err)
	cleanup.AddCleanTasks(t, hostAwait.Client, userSignup)
	emailAnnotation := userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey]
	assert.Equal(t, emailAddress, emailAnnotation)

	// Call get signup endpoint with a valid token and make sure verificationRequired is true
	mp, mpStatus := ParseResponse(t, InvokeEndpoint(t, "GET", route+"/api/v1/signup", token0, "", http.StatusOK))
	assert.Equal(t, "", mp["compliantUsername"])
	assert.Equal(t, identity0.Username, mp["username"])
	require.IsType(t, false, mpStatus["ready"])
	assert.False(t, mpStatus["ready"].(bool))
	assert.Equal(t, "PendingApproval", mpStatus["reason"])
	require.True(t, mpStatus["verificationRequired"].(bool))

	// Confirm the status of the UserSignup is correct
	_, err = hostAwait.WaitForUserSignup(t, identity0.Username,
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(t, err)

	// Confirm that a MUR hasn't been created
	obj := &toolchainv1alpha1.MasterUserRecord{}
	err = hostAwait.Client.Get(context.TODO(), types.NamespacedName{Namespace: hostAwait.Namespace, Name: identity0.Username}, obj)
	require.Error(t, err)
	require.True(t, errors.IsNotFound(err))

	// Initiate the verification process
	InvokeEndpoint(t, "PUT", route+"/api/v1/signup/verification", token0,
		`{ "country_code":"+61", "phone_number":"408999999" }`, http.StatusNoContent)

	// Retrieve the updated UserSignup
	userSignup, err = hostAwait.WaitForUserSignup(t, identity0.Username)
	require.NoError(t, err)

	// Confirm there is a verification code annotation value, and store it in a variable
	verificationCode := userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey]
	require.NotEmpty(t, verificationCode)

	// Confirm the expiry time has been set
	require.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserVerificationExpiryAnnotationKey])

	// Attempt to verify with an incorrect verification code
	InvokeEndpoint(t, "GET", route+"/api/v1/signup/verification/invalid", token0, "", http.StatusForbidden)

	// Retrieve the updated UserSignup
	userSignup, err = hostAwait.WaitForUserSignup(t, identity0.Username)
	require.NoError(t, err)

	// Check attempts has been incremented
	require.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserVerificationAttemptsAnnotationKey])

	// Confirm the verification code has not changed
	require.Equal(t, verificationCode, userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey])

	// Verify with the correct code
	InvokeEndpoint(t, "GET", route+fmt.Sprintf("/api/v1/signup/verification/%s",
		userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey]), token0, "", http.StatusOK)

	// Retrieve the updated UserSignup
	userSignup, err = hostAwait.WaitForUserSignup(t, identity0.Username,
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValuePending))
	require.NoError(t, err)

	// Confirm all unrequired verification-related annotations have been removed
	require.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserVerificationExpiryAnnotationKey])
	require.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserVerificationAttemptsAnnotationKey])
	require.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey])
	require.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationTimestampAnnotationKey])
	require.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCounterAnnotationKey])
	require.Empty(t, userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationInitTimestampAnnotationKey])

	// Call get signup endpoint with a valid token and make sure it's pending approval
	mp, mpStatus = ParseResponse(t, InvokeEndpoint(t, "GET", route+"/api/v1/signup", token0, "", http.StatusOK))
	assert.Equal(t, "", mp["compliantUsername"])
	assert.Equal(t, "", mp["defaultUserNamespace"])
	assert.Equal(t, "", mp["rhodsMemberURL"])
	assert.Equal(t, identity0.Username, mp["username"])
	require.IsType(t, false, mpStatus["ready"])
	assert.False(t, mpStatus["ready"].(bool))
	assert.Equal(t, "PendingApproval", mpStatus["reason"])
	require.False(t, mpStatus["verificationRequired"].(bool))

	userSignup, err = hostAwait.UpdateUserSignup(t, userSignup.Name,
		func(instance *toolchainv1alpha1.UserSignup) {
			// Now approve the usersignup.
			states.SetApprovedManually(instance, true)
		})
	require.NoError(t, err)
	transformedUsername := commonsignup.TransformUsername(userSignup.Spec.Username, []string{"openshift", "kube", "default", "redhat", "sandbox"}, []string{"admin"})
	// Confirm the MasterUserRecord is provisioned
	_, err = hostAwait.WaitForMasterUserRecord(t, transformedUsername, wait.UntilMasterUserRecordHasCondition(wait.Provisioned()))
	require.NoError(t, err)

	// Retrieve the UserSignup from the GET endpoint
	_, mpStatus = ParseResponse(t, InvokeEndpoint(t, "GET", route+"/api/v1/signup", token0, "", http.StatusOK))

	// Confirm that VerificationRequired is no longer true
	require.False(t, mpStatus["verificationRequired"].(bool))

	// Create another token and identity to sign up with
	otherEmailValue := uuid.Must(uuid.NewV4()).String() + "@other.domain"
	otherIdentity, otherToken, err := authsupport.NewToken(authsupport.WithEmail(otherEmailValue))
	require.NoError(t, err)

	// Call the signup endpoint
	InvokeEndpoint(t, "POST", route+"/api/v1/signup", otherToken, "", http.StatusAccepted)

	// Wait for the UserSignup to be created
	otherUserSignup, err := hostAwait.WaitForUserSignup(t, otherIdentity.Username,
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(t, err)
	cleanup.AddCleanTasks(t, hostAwait.Client, otherUserSignup)
	otherEmailAnnotation := otherUserSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey]
	assert.Equal(t, otherEmailValue, otherEmailAnnotation)

	// Initiate the verification process using the same phone number as previously
	responseMap := InvokeEndpoint(t, "PUT", route+"/api/v1/signup/verification", otherToken,
		`{ "country_code":"+61", "phone_number":"408999999" }`, http.StatusForbidden)

	require.NotEmpty(t, responseMap)
	require.Equal(t, float64(http.StatusForbidden), responseMap["code"], "code not found in response body map %s", responseMap)

	require.Equal(t, "Forbidden", responseMap["status"])
	require.Equal(t, "phone number already in use: cannot register using phone number: +61408999999", responseMap["message"])
	require.Equal(t, "phone number already in use", responseMap["details"])

	// Retrieve the updated UserSignup
	otherUserSignup, err = hostAwait.WaitForUserSignup(t, otherIdentity.Username)
	require.NoError(t, err)

	// Confirm there is no verification code annotation value
	require.Empty(t, otherUserSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey])

	// Retrieve the current UserSignup
	userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name)
	require.NoError(t, err)

	userSignup, err = hostAwait.UpdateUserSignup(t, userSignup.Name,
		func(instance *toolchainv1alpha1.UserSignup) {
			// Now mark the original UserSignup as deactivated
			states.SetDeactivated(instance, true)
		})
	require.NoError(t, err)

	// Ensure the UserSignup is deactivated
	_, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin(), wait.ManuallyDeactivated())...))
	require.NoError(t, err)

	// Now attempt the verification again
	InvokeEndpoint(t, "PUT", route+"/api/v1/signup/verification", otherToken,
		`{ "country_code":"+61", "phone_number":"408999999" }`, http.StatusNoContent)

	// Retrieve the updated UserSignup again
	otherUserSignup, err = hostAwait.WaitForUserSignup(t, otherIdentity.Username)
	require.NoError(t, err)

	// Confirm there is now a verification code annotation value
	require.NotEmpty(t, otherUserSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey])
}

func TestActivationCodeVerification(t *testing.T) {
	// given
	t.Parallel()
	await := WaitForDeployments(t)
	hostAwait := await.Host()
	route := hostAwait.RegistrationServiceURL

	t.Run("verification successful", func(t *testing.T) {
		// given
		event := testsocialevent.NewSocialEvent(hostAwait.Namespace, commonsocialevent.NewName(),
			testsocialevent.WithUserTier("deactivate80"),
			testsocialevent.WithSpaceTier("base1ns6didler"))
		err := hostAwait.CreateWithCleanup(t, event)
		require.NoError(t, err)
		userSignup, token := signup(t, hostAwait)

		// when call verification endpoint with a valid activation code
		InvokeEndpoint(t, "POST", route+"/api/v1/signup/verification/activation-code", token, fmt.Sprintf(`{"code":"%s"}`, event.Name), http.StatusOK)

		// then
		// ensure the UserSignup is in "pending approval" condition,
		// because in these series of parallel tests, automatic approval is disabled ¯\_(ツ)_/¯
		_, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasLabel(toolchainv1alpha1.SocialEventUserSignupLabelKey, event.Name),
			wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.PendingApproval())...))
		require.NoError(t, err)
		// explicitly approve the usersignup (see above, config for parallel test has automatic approval disabled)
		userSignup, err = hostAwait.UpdateUserSignup(t, userSignup.Name,
			func(us *toolchainv1alpha1.UserSignup) {
				states.SetApprovedManually(us, true)
			})
		require.NoError(t, err)
		t.Logf("user signup '%s' approved", userSignup.Name)

		// check that the MUR and Space are configured as expected
		// Wait for the UserSignup to have the desired state
		userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasCompliantUsername())
		require.NoError(t, err)
		mur, err := hostAwait.WaitForMasterUserRecord(t, userSignup.Status.CompliantUsername,
			wait.UntilMasterUserRecordHasTierName(event.Spec.UserTier),
			wait.UntilMasterUserRecordHasCondition(wait.Provisioned()))
		require.NoError(t, err)
		assert.Equal(t, event.Spec.UserTier, mur.Spec.TierName)
		_, err = hostAwait.WaitForSpace(t, userSignup.Status.CompliantUsername,
			wait.UntilSpaceHasTier(event.Spec.SpaceTier),
			wait.UntilSpaceHasConditions(wait.Provisioned()),
		)
		require.NoError(t, err)

		// also check that the SocialEvent status was updated accordingly
		_, err = hostAwait.WaitForSocialEvent(t, event.Name, wait.UntilSocialEventHasActivationCount(1))
		require.NoError(t, err)
	})

	t.Run("verification failed", func(t *testing.T) {

		t.Run("unknown code", func(t *testing.T) {
			// given
			userSignup, token := signup(t, hostAwait)

			// when call verification endpoint with a valid activation code
			InvokeEndpoint(t, "POST", route+"/api/v1/signup/verification/activation-code", token, fmt.Sprintf(`{"code":"%s"}`, "unknown"), http.StatusForbidden)

			// then
			// ensure the UserSignup is not approved yet
			userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...))
			require.NoError(t, err)
			assert.Equal(t, userSignup.Annotations[toolchainv1alpha1.UserVerificationAttemptsAnnotationKey], "1")
		})

		t.Run("over capacity", func(t *testing.T) {
			// given
			event := testsocialevent.NewSocialEvent(hostAwait.Namespace, commonsocialevent.NewName(),
				testsocialevent.WithUserTier("deactivate80"),
				testsocialevent.WithSpaceTier("base1ns6didler"))
			err := hostAwait.CreateWithCleanup(t, event)
			require.NoError(t, err)
			event, err = hostAwait.WaitForSocialEvent(t, event.Name) // need to reload event
			require.NoError(t, err)
			event.Status.ActivationCount = event.Spec.MaxAttendees // activation count identical to `MaxAttendees`
			err = hostAwait.Client.Status().Update(context.TODO(), event)
			require.NoError(t, err)

			userSignup, token := signup(t, hostAwait)

			// when call verification endpoint with a valid activation code
			InvokeEndpoint(t, "POST", route+"/api/v1/signup/verification/activation-code", token, fmt.Sprintf(`{"code":"%s"}`, event.Name), http.StatusForbidden)

			// then
			// ensure the UserSignup is not approved yet
			userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...))
			require.NoError(t, err)
			assert.Equal(t, userSignup.Annotations[toolchainv1alpha1.UserVerificationAttemptsAnnotationKey], "1")
		})

		t.Run("not opened yet", func(t *testing.T) {
			// given
			event := testsocialevent.NewSocialEvent(hostAwait.Namespace, commonsocialevent.NewName(), testsocialevent.WithStartTime(time.Now().Add(time.Hour))) // not open yet
			err := hostAwait.CreateWithCleanup(t, event)
			require.NoError(t, err)
			userSignup, token := signup(t, hostAwait)

			// when call verification endpoint with a valid activation code
			InvokeEndpoint(t, "POST", route+"/api/v1/signup/verification/activation-code", token, fmt.Sprintf(`{"code":"%s"}`, event.Name), http.StatusForbidden)

			// then
			// ensure the UserSignup is not approved yet
			userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...))
			require.NoError(t, err)
			assert.Equal(t, userSignup.Annotations[toolchainv1alpha1.UserVerificationAttemptsAnnotationKey], "1")
		})

		t.Run("already closed", func(t *testing.T) {
			// given
			event := testsocialevent.NewSocialEvent(hostAwait.Namespace, commonsocialevent.NewName(), testsocialevent.WithEndTime(time.Now().Add(-time.Hour))) // already closd
			err := hostAwait.CreateWithCleanup(t, event)
			require.NoError(t, err)
			userSignup, token := signup(t, hostAwait)

			// when call verification endpoint with a valid activation code
			InvokeEndpoint(t, "POST", route+"/api/v1/signup/verification/activation-code", token, fmt.Sprintf(`{"code":"%s"}`, event.Name), http.StatusForbidden)

			// then
			// ensure the UserSignup is approved
			userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...))
			require.NoError(t, err)
			assert.Equal(t, userSignup.Annotations[toolchainv1alpha1.UserVerificationAttemptsAnnotationKey], "1")
		})

		t.Run("invalid code", func(t *testing.T) {

		})
	})
}

func signup(t *testing.T, hostAwait *wait.HostAwaitility) (*toolchainv1alpha1.UserSignup, string) {
	route := hostAwait.RegistrationServiceURL

	// Create a token and identity to sign up with
	identity := commonauth.NewIdentity()
	emailValue := identity.Username + "@some.domain"
	emailClaim := commonauth.WithEmailClaim(emailValue)
	token, err := commonauth.GenerateSignedE2ETestToken(*identity, emailClaim)
	require.NoError(t, err)

	// Call the signup endpoint
	InvokeEndpoint(t, "POST", route+"/api/v1/signup", token, "", http.StatusAccepted)

	// Wait for the UserSignup to be created
	userSignup, err := hostAwait.WaitForUserSignup(t, identity.Username,
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(t, err)
	cleanup.AddCleanTasks(t, hostAwait.Client, userSignup)
	emailAnnotation := userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey]
	assert.Equal(t, emailValue, emailAnnotation)
	return userSignup, token
}

func assertGetSignupStatusProvisioned(t *testing.T, await wait.Awaitilities, username, bearerToken string) {
	hostAwait := await.Host()
	memberAwait := await.Member1()
	mp := waitForUserSignupReadyInRegistrationService(t, hostAwait.RegistrationServiceURL, username, bearerToken)
	transformedUsername := commonsignup.TransformUsername(username, []string{"openshift", "kube", "default", "redhat", "sandbox"}, []string{"admin"})
	assert.Equal(t, transformedUsername, mp["compliantUsername"])
	assert.Equal(t, username, mp["username"])
	assert.Equal(t, memberAwait.GetConsoleURL(t), mp["consoleURL"])
	memberCluster, found, err := hostAwait.GetToolchainCluster(t, cluster.Member, memberAwait.Namespace, nil)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, memberCluster.Spec.APIEndpoint, mp["apiEndpoint"])
	assert.Equal(t, hostAwait.APIProxyURL, mp["proxyURL"])
	assert.Equal(t, fmt.Sprintf("%s-dev", transformedUsername), mp["defaultUserNamespace"])
	assertRHODSClusterURL(t, memberAwait, mp)
}

func assertGetSignupStatusPendingApproval(t *testing.T, await wait.Awaitilities, username, bearerToken string) {
	route := await.Host().RegistrationServiceURL
	mp, mpStatus := ParseResponse(t, InvokeEndpoint(t, "GET", route+"/api/v1/signup", bearerToken, "", http.StatusOK))
	assert.Equal(t, username, mp["username"])
	assert.Empty(t, mp["defaultUserNamespace"])
	assert.Empty(t, mp["rhodsMemberURL"])
	require.IsType(t, false, mpStatus["ready"])
	assert.False(t, mpStatus["ready"].(bool))
	assert.Equal(t, "PendingApproval", mpStatus["reason"])
}

func assertGetSignupReturnsNotFound(t *testing.T, await wait.Awaitilities, bearerToken string) {
	route := await.Host().RegistrationServiceURL
	InvokeEndpoint(t, "GET", route+"/api/v1/signup", bearerToken, "", http.StatusNotFound)
}

func assertRHODSClusterURL(t *testing.T, memberAwait *wait.MemberAwaitility, response map[string]interface{}) {
	require.Containsf(t, memberAwait.GetConsoleURL(t), ".apps.", "expected to find .apps. in the console URL %s", memberAwait.GetConsoleURL(t))
	index := strings.Index(memberAwait.GetConsoleURL(t), ".apps.")
	appsURL := memberAwait.GetConsoleURL(t)[index:]
	assert.Equal(t, fmt.Sprintf("https://%s%s", "rhods-dashboard-redhat-ods-applications", appsURL), response["rhodsMemberURL"])
}

// waitForUserSignupReadyInRegistrationService waits and checks that the UserSignup is ready according to registration service /signup endpoint
func waitForUserSignupReadyInRegistrationService(t *testing.T, registrationServiceURL, name, bearerToken string) map[string]interface{} {
	t.Logf("waiting and verifying that UserSignup '%s' is ready according to registration service", name)
	var mp, mpStatus map[string]interface{}
	err := k8swait.Poll(time.Second, time.Second*60, func() (done bool, err error) {
		mp, mpStatus = ParseResponse(t, InvokeEndpoint(t, "GET", registrationServiceURL+"/api/v1/signup", bearerToken, "", http.StatusOK))
		// check if `ready` field is set
		if _, ok := mpStatus["ready"]; !ok {
			t.Logf("usersignup response for %s is missing `ready` field ", name)
			t.Logf("registration service status response: %s", spew.Sdump(mpStatus))
			return false, nil
		}
		// if `ready` field is not true,
		// means that user signup is not "ready"
		if !mpStatus["ready"].(bool) {
			t.Logf("usersignup %s is not ready yet according to registration service", name)
			t.Logf("registration service status response: %s", spew.Sdump(mpStatus))
			return false, nil
		}
		// check signup status reason
		if mpStatus["reason"] != toolchainv1alpha1.MasterUserRecordProvisionedReason {
			t.Logf("usersignup %s is not Provisioned yet according to registration service", name)
			t.Logf("registration service status response: %s", spew.Sdump(mpStatus))
			return false, nil
		}

		return true, nil
	})
	require.NoError(t, err)
	return mp
}
