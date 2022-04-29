package parallel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/cleanup"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

		body, err := ioutil.ReadAll(resp.Body)
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

func TestWoopra(t *testing.T) {
	// given
	t.Parallel()
	await := WaitForDeployments(t)
	route := await.Host().RegistrationServiceURL

	assertNotSecuredGetResponseEquals := func(endPointPath, expectedResponseValue string) {
		// Call woopra domain endpoint.
		req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/%s", route, endPointPath), nil)
		require.NoError(t, err)

		resp, err := httpClient.Do(req) //nolint:bodyclose // see `defer Close(...)`
		require.NoError(t, err)
		defer Close(t, resp)

		assert.Equal(t, http.StatusOK, resp.StatusCode)

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NotNil(t, body)

		value := string(body)
		require.NoError(t, err)

		// Verify JSON response.
		require.Equal(t, expectedResponseValue, value)
	}

	t.Run("get woopra domain 200 OK", func(t *testing.T) {
		// Call woopra domain endpoint.
		assertNotSecuredGetResponseEquals("woopra-domain", "test woopra domain")
	})

	t.Run("get segment write key 200 OK", func(t *testing.T) {
		// Call segment write key endpoint.
		assertNotSecuredGetResponseEquals("segment-write-key", "test segment write key")
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

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NotNil(t, body)
	})
}

func TestSignupFails(t *testing.T) {
	// given
	t.Parallel()
	await := WaitForDeployments(t)
	route := await.Host().RegistrationServiceURL

	identity0 := authsupport.NewIdentity()
	emailClaim0 := authsupport.WithEmailClaim(uuid.Must(uuid.NewV4()).String() + "@acme.com")

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

		body, err := ioutil.ReadAll(resp.Body)
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
		mp := invokeEndpoint(t, "POST", route+"/api/v1/signup", "1223123123", "", http.StatusUnauthorized)

		// Check token error.
		tokenErr := mp["error"]
		require.Equal(t, "token contains an invalid number of segments", tokenErr.(string))
	})
	t.Run("post signup exp token 401 Unauthorized", func(t *testing.T) {
		expClaim1 := authsupport.WithExpClaim(time.Now().Add(-60 * time.Second))

		// Not identical to the token used in POST signup - should return resource not found.
		token1, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0, expClaim1)
		require.NoError(t, err)
		mp := invokeEndpoint(t, "POST", route+"/api/v1/signup", token1, "", http.StatusUnauthorized)

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

		body, err := ioutil.ReadAll(resp.Body)
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
		mp := invokeEndpoint(t, "GET", route+"/api/v1/signup", "1223123123", "", http.StatusUnauthorized)

		// Check token error.
		tokenErr := mp["error"]
		require.Equal(t, "token contains an invalid number of segments", tokenErr.(string))
	})
	t.Run("get signup exp token 401 Unauthorized", func(t *testing.T) {
		expClaim1 := authsupport.WithExpClaim(time.Now().Add(-60 * time.Second))

		// Not identical to the token used in POST signup - should return resource not found.
		token1, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0, expClaim1)
		require.NoError(t, err)
		mp := invokeEndpoint(t, "GET", route+"/api/v1/signup", token1, "", http.StatusUnauthorized)

		// Check token error.
		tokenErr := mp["error"]
		require.Contains(t, tokenErr.(string), "token is expired by ")
	})
	t.Run("get signup 404 NotFound", func(t *testing.T) {
		// Get valid generated token for e2e tests. IAT claim is overridden
		// to avoid token used before issued error.
		identity1 := authsupport.NewIdentity()
		emailClaim1 := authsupport.WithEmailClaim(uuid.Must(uuid.NewV4()).String() + "@acme.com")
		iatClaim1 := authsupport.WithIATClaim(time.Now().Add(-60 * time.Second))

		// Not identical to the token used in POST signup - should return resource not found.
		token1, err := authsupport.GenerateSignedE2ETestToken(*identity1, emailClaim1, iatClaim1)
		require.NoError(t, err)

		// Call signup endpoint with a valid token.
		assertGetSignupReturnsNotFound(t, await, token1)
	})

	t.Run("get signup for crtadmin fails", func(t *testing.T) {
		// Get valid generated token for e2e tests. IAT claim is overridden
		// to avoid token used before issued error. Username claim is also
		// overridden to trigger error and ensure that usersignup is not created.
		identity := authsupport.NewIdentity()
		emailValue := uuid.Must(uuid.NewV4()).String() + "@acme.com"
		emailClaim := authsupport.WithEmailClaim(emailValue)
		usernameClaim := authsupport.WithPreferredUsernameClaim("test-crtadmin")
		token, err := authsupport.GenerateSignedE2ETestToken(*identity, emailClaim, usernameClaim)
		require.NoError(t, err)

		// Call signup endpoint with a valid token to initiate a signup process
		response := invokeEndpoint(t, "POST", route+"/api/v1/signup", token, "", http.StatusForbidden)
		require.Equal(t, "forbidden: failed to create usersignup for test-crtadmin", response["message"])
		require.Equal(t, "error creating UserSignup resource", response["details"])
		require.Equal(t, float64(403), response["code"])

		hostAwait := await.Host()
		hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second * 15)).WaitAndVerifyThatUserSignupIsNotCreated(identity.ID.String())
	})
}

func TestSignupOK(t *testing.T) {
	// given
	t.Parallel()
	await := WaitForDeployments(t)
	route := await.Host().RegistrationServiceURL

	hostAwait := await.Host()
	memberAwait := await.Member1()
	signupUser := func(token, email, userSignupName string, identity *authsupport.Identity) *toolchainv1alpha1.UserSignup {
		// Call signup endpoint with a valid token to initiate a signup process
		invokeEndpoint(t, "POST", route+"/api/v1/signup", token, "", http.StatusAccepted)

		// Wait for the UserSignup to be created
		userSignup, err := hostAwait.WaitForUserSignup(userSignupName,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), PendingApproval())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValuePending))
		require.NoError(t, err)
		cleanup.AddCleanTasks(hostAwait, userSignup)
		emailAnnotation := userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey]
		assert.Equal(t, email, emailAnnotation)

		// Call get signup endpoint with a valid token and make sure it's pending approval
		assertGetSignupStatusPendingApproval(t, await, identity.Username, token)

		// Attempt to create same usersignup by calling post signup with same token should return an error
		mp := invokeEndpoint(t, "POST", route+"/api/v1/signup", token, "", http.StatusConflict)
		assert.Equal(t, fmt.Sprintf("Operation cannot be fulfilled on  \"\": UserSignup [id: %s; username: %s]. Unable to create UserSignup because there is already an active UserSignup with such ID",
			identity.ID, identity.Username), mp["message"])
		assert.Equal(t, "error creating UserSignup resource", mp["details"])

		// Approve usersignup.
		states.SetApproved(userSignup, true)
		userSignup.Spec.TargetCluster = memberAwait.ClusterName
		err = hostAwait.Client.Update(context.TODO(), userSignup)
		require.NoError(t, err)

		// Wait the Master User Record to be provisioned
		VerifyResourcesProvisionedForSignup(t, await, userSignup, "base", "base")

		// Call signup endpoint with same valid token to check if status changed to Provisioned now
		assertGetSignupStatusProvisioned(t, await, identity.Username, token)

		return userSignup
	}

	t.Run("test activation-deactivation workflow", func(t *testing.T) {
		// Get valid generated token for e2e tests. IAT claim is overridden
		// to avoid token used before issued error.
		identity := authsupport.NewIdentity()
		emailValue := uuid.Must(uuid.NewV4()).String() + "@acme.com"
		emailClaim := authsupport.WithEmailClaim(emailValue)
		token, err := authsupport.GenerateSignedE2ETestToken(*identity, emailClaim)
		require.NoError(t, err)

		// Signup a new user
		userSignup := signupUser(token, emailValue, identity.Username, identity)

		// Deactivate the usersignup
		userSignup, err = hostAwait.UpdateUserSignup(userSignup.Name, func(us *toolchainv1alpha1.UserSignup) {
			states.SetDeactivated(us, true)
		})
		require.NoError(t, err)
		_, err = hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), DeactivatedWithoutPreDeactivation())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
		require.NoError(t, err)

		// Now check that the reg-service treats the deactivated usersignup as nonexistent and returns 404
		assertGetSignupReturnsNotFound(t, await, token)

		// Re-activate the usersignup by calling the signup endpoint with the same token/user again
		signupUser(token, emailValue, identity.Username, identity)
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
	identity0 := authsupport.NewIdentity()
	emailValue := "arnold@acme.com"
	emailClaim0 := authsupport.WithEmailClaim(emailValue)
	token0, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0, authsupport.WithSubClaim(identity0.ID.String()),
		authsupport.WithPreferredUsernameClaim("arnold"))
	require.NoError(t, err)

	// Call the signup endpoint
	invokeEndpoint(t, "POST", route+"/api/v1/signup", token0, "", http.StatusAccepted)

	// Wait for the UserSignup to be created
	userSignup, err := hostAwait.WaitForUserSignup("arnold",
		wait.UntilUserSignupHasConditions(ConditionSet(Default(), PendingApproval())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValuePending))
	require.NoError(t, err)
	cleanup.AddCleanTasks(hostAwait, userSignup)
	emailAnnotation := userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey]
	assert.Equal(t, emailValue, emailAnnotation)

	// Call get signup endpoint with a valid token, however we will now override the claims to introduce the original
	// sub claim and set username as a separate claim, then we will make sure the UserSignup is returned correctly
	token0, err = authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0, authsupport.WithPreferredUsernameClaim("arnold"))
	require.NoError(t, err)
	mp, mpStatus := parseResponse(t, invokeEndpoint(t, "GET", route+"/api/v1/signup", token0, "", http.StatusOK))
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
	identity0 := authsupport.NewIdentity()
	emailValue := uuid.Must(uuid.NewV4()).String() + "@some.domain"
	emailClaim0 := authsupport.WithEmailClaim(emailValue)
	token0, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0)
	require.NoError(t, err)

	// Call the signup endpoint
	invokeEndpoint(t, "POST", route+"/api/v1/signup", token0, "", http.StatusAccepted)

	// Wait for the UserSignup to be created
	userSignup, err := hostAwait.WaitForUserSignup(identity0.Username,
		wait.UntilUserSignupHasConditions(ConditionSet(Default(), VerificationRequired())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(t, err)
	cleanup.AddCleanTasks(hostAwait, userSignup)
	emailAnnotation := userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey]
	assert.Equal(t, emailValue, emailAnnotation)

	// Call get signup endpoint with a valid token and make sure verificationRequired is true
	mp, mpStatus := parseResponse(t, invokeEndpoint(t, "GET", route+"/api/v1/signup", token0, "", http.StatusOK))
	assert.Equal(t, "", mp["compliantUsername"])
	assert.Equal(t, identity0.Username, mp["username"])
	require.IsType(t, false, mpStatus["ready"])
	assert.False(t, mpStatus["ready"].(bool))
	assert.Equal(t, "PendingApproval", mpStatus["reason"])
	require.True(t, mpStatus["verificationRequired"].(bool))

	// Confirm the status of the UserSignup is correct
	_, err = hostAwait.WaitForUserSignup(identity0.Username,
		wait.UntilUserSignupHasConditions(ConditionSet(Default(), VerificationRequired())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(t, err)

	// Confirm that a MUR hasn't been created
	obj := &toolchainv1alpha1.MasterUserRecord{}
	err = hostAwait.Client.Get(context.TODO(), types.NamespacedName{Namespace: hostAwait.Namespace, Name: identity0.Username}, obj)
	require.Error(t, err)
	require.True(t, errors.IsNotFound(err))

	// Initiate the verification process
	invokeEndpoint(t, "PUT", route+"/api/v1/signup/verification", token0,
		`{ "country_code":"+61", "phone_number":"408999999" }`, http.StatusNoContent)

	// Retrieve the updated UserSignup
	userSignup, err = hostAwait.WaitForUserSignup(identity0.Username)
	require.NoError(t, err)

	// Confirm there is a verification code annotation value, and store it in a variable
	verificationCode := userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey]
	require.NotEmpty(t, verificationCode)

	// Confirm the expiry time has been set
	require.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserVerificationExpiryAnnotationKey])

	// Attempt to verify with an incorrect verification code
	invokeEndpoint(t, "GET", route+"/api/v1/signup/verification/invalid", token0, "", http.StatusForbidden)

	// Retrieve the updated UserSignup
	userSignup, err = hostAwait.WaitForUserSignup(identity0.Username)
	require.NoError(t, err)

	// Check attempts has been incremented
	require.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserVerificationAttemptsAnnotationKey])

	// Confirm the verification code has not changed
	require.Equal(t, verificationCode, userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey])

	// Verify with the correct code
	invokeEndpoint(t, "GET", route+fmt.Sprintf("/api/v1/signup/verification/%s",
		userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey]), token0, "", http.StatusOK)

	// Retrieve the updated UserSignup
	userSignup, err = hostAwait.WaitForUserSignup(identity0.Username,
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
	mp, mpStatus = parseResponse(t, invokeEndpoint(t, "GET", route+"/api/v1/signup", token0, "", http.StatusOK))
	assert.Equal(t, "", mp["compliantUsername"])
	assert.Equal(t, identity0.Username, mp["username"])
	require.IsType(t, false, mpStatus["ready"])
	assert.False(t, mpStatus["ready"].(bool))
	assert.Equal(t, "PendingApproval", mpStatus["reason"])
	require.False(t, mpStatus["verificationRequired"].(bool))

	// Now approve the usersignup.
	states.SetApproved(userSignup, true)

	err = hostAwait.Client.Update(context.TODO(), userSignup)
	require.NoError(t, err)

	// Confirm the MasterUserRecord is provisioned
	_, err = hostAwait.WaitForMasterUserRecord(identity0.Username, wait.UntilMasterUserRecordHasCondition(Provisioned()))
	require.NoError(t, err)

	// Retrieve the UserSignup from the GET endpoint
	_, mpStatus = parseResponse(t, invokeEndpoint(t, "GET", route+"/api/v1/signup", token0, "", http.StatusOK))

	// Confirm that VerificationRequired is no longer true
	require.False(t, mpStatus["verificationRequired"].(bool))

	// Create another token and identity to sign up with
	otherIdentity := authsupport.NewIdentity()
	otherEmailValue := uuid.Must(uuid.NewV4()).String() + "@other.domain"
	otherEmailClaim := authsupport.WithEmailClaim(otherEmailValue)
	otherToken, err := authsupport.GenerateSignedE2ETestToken(*otherIdentity, otherEmailClaim)
	require.NoError(t, err)

	// Call the signup endpoint
	invokeEndpoint(t, "POST", route+"/api/v1/signup", otherToken, "", http.StatusAccepted)

	// Wait for the UserSignup to be created
	otherUserSignup, err := hostAwait.WaitForUserSignup(otherIdentity.Username,
		wait.UntilUserSignupHasConditions(ConditionSet(Default(), VerificationRequired())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(t, err)
	cleanup.AddCleanTasks(hostAwait, otherUserSignup)
	otherEmailAnnotation := otherUserSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey]
	assert.Equal(t, otherEmailValue, otherEmailAnnotation)

	// Initiate the verification process using the same phone number as previously
	responseMap := invokeEndpoint(t, "PUT", route+"/api/v1/signup/verification", otherToken,
		`{ "country_code":"+61", "phone_number":"408999999" }`, http.StatusForbidden)

	require.NotEmpty(t, responseMap)
	require.Equal(t, float64(http.StatusForbidden), responseMap["code"], "code not found in response body map %s", responseMap)

	require.Equal(t, "Forbidden", responseMap["status"])
	require.Equal(t, "phone number already in use:cannot register using phone number: +61408999999", responseMap["message"])
	require.Equal(t, "phone number already in use", responseMap["details"])

	// Retrieve the updated UserSignup
	otherUserSignup, err = hostAwait.WaitForUserSignup(otherIdentity.Username)
	require.NoError(t, err)

	// Confirm there is no verification code annotation value
	require.Empty(t, otherUserSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey])

	// Retrieve the current UserSignup
	userSignup, err = hostAwait.WaitForUserSignup(userSignup.Name)
	require.NoError(t, err)

	// Now mark the original UserSignup as deactivated
	states.SetDeactivated(userSignup, true)

	err = hostAwait.Client.Update(context.TODO(), userSignup)
	require.NoError(t, err)

	// Ensure the UserSignup is deactivated
	_, err = hostAwait.WaitForUserSignup(userSignup.Name,
		wait.UntilUserSignupHasConditions(ConditionSet(Default(), ApprovedByAdmin(), ManuallyDeactivated())...))
	require.NoError(t, err)

	// Now attempt the verification again
	invokeEndpoint(t, "PUT", route+"/api/v1/signup/verification", otherToken,
		`{ "country_code":"+61", "phone_number":"408999999" }`, http.StatusNoContent)

	// Retrieve the updated UserSignup again
	otherUserSignup, err = hostAwait.WaitForUserSignup(otherIdentity.Username)
	require.NoError(t, err)

	// Confirm there is now a verification code annotation value
	require.NotEmpty(t, otherUserSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey])
}

func assertGetSignupStatusProvisioned(t *testing.T, await wait.Awaitilities, username, bearerToken string) {
	hostAwait := await.Host()
	route := await.Host().RegistrationServiceURL
	memberAwait := await.Member1()
	mp, mpStatus := parseResponse(t, invokeEndpoint(t, "GET", route+"/api/v1/signup", bearerToken, "", http.StatusOK))
	assert.Equal(t, username, mp["compliantUsername"])
	assert.Equal(t, username, mp["username"])
	require.IsType(t, false, mpStatus["ready"])
	assert.True(t, mpStatus["ready"].(bool))
	assert.Equal(t, "Provisioned", mpStatus["reason"])
	assert.Equal(t, memberAwait.GetConsoleURL(), mp["consoleURL"])
	memberCluster, found, err := hostAwait.GetToolchainCluster(cluster.Member, memberAwait.Namespace, nil)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, memberCluster.Spec.APIEndpoint, mp["apiEndpoint"])
	assert.Equal(t, hostAwait.APIProxyURL, mp["proxyURL"])
}

func assertGetSignupStatusPendingApproval(t *testing.T, await wait.Awaitilities, username, bearerToken string) {
	route := await.Host().RegistrationServiceURL
	mp, mpStatus := parseResponse(t, invokeEndpoint(t, "GET", route+"/api/v1/signup", bearerToken, "", http.StatusOK))
	assert.Equal(t, username, mp["username"])
	require.IsType(t, false, mpStatus["ready"])
	assert.False(t, mpStatus["ready"].(bool))
	assert.Equal(t, "PendingApproval", mpStatus["reason"])
}

func assertGetSignupReturnsNotFound(t *testing.T, await wait.Awaitilities, bearerToken string) {
	route := await.Host().RegistrationServiceURL
	invokeEndpoint(t, "GET", route+"/api/v1/signup", bearerToken, "", http.StatusNotFound)
}

func invokeEndpoint(t *testing.T, method, path, authToken, requestBody string, requiredStatus int) map[string]interface{} {
	var reqBody io.Reader
	if requestBody != "" {
		reqBody = strings.NewReader(requestBody)
	}
	req, err := http.NewRequest(method, path, reqBody)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("content-type", "application/json")
	resp, err := httpClient.Do(req) // nolint:bodyclose // see `defer.Close(...)`
	require.NoError(t, err)
	defer Close(t, resp)

	body, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NotNil(t, body)
	require.Equal(t, requiredStatus, resp.StatusCode, "unexpected response status with body: %s", body)

	mp := make(map[string]interface{})
	if len(body) > 0 {
		err = json.Unmarshal(body, &mp)
		require.NoError(t, err)
	}
	return mp
}

func parseResponse(t *testing.T, responseBody map[string]interface{}) (map[string]interface{}, map[string]interface{}) {
	// Check that the response looks fine
	status, ok := responseBody["status"].(map[string]interface{})
	require.True(t, ok)
	return responseBody, status
}
