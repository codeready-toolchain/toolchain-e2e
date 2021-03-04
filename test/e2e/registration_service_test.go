package e2e

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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

var httpClient = HttpClient

func TestRegistrationService(t *testing.T) {
	suite.Run(t, &registrationServiceTestSuite{})
}

type registrationServiceTestSuite struct {
	suite.Suite
	namespace   string
	route       string
	ctx         *framework.Context
	hostAwait   *wait.HostAwaitility
	memberAwait *wait.MemberAwaitility
}

func (s *registrationServiceTestSuite) SetupSuite() {
	userSignupList := &v1alpha1.UserSignupList{}
	s.ctx, s.hostAwait, s.memberAwait, _ = WaitForDeployments(s.T(), userSignupList)
	s.namespace = s.hostAwait.RegistrationServiceNs
	s.route = s.hostAwait.RegistrationServiceURL
}

func (s *registrationServiceTestSuite) TestLandingPageReachable() {
	// just make sure that the landing page is reachable
	req, err := http.NewRequest("GET", s.route, nil)
	require.NoError(s.T(), err)

	resp, err := httpClient.Do(req)
	require.NoError(s.T(), err)
	defer close(s.T(), resp)

	assert.Equal(s.T(), http.StatusOK, resp.StatusCode)
}

func (s *registrationServiceTestSuite) TestHealth() {
	s.Run("get healthcheck 200 OK", func() {
		// Call health endpoint.
		req, err := http.NewRequest("GET", s.route+"/api/v1/health", nil)
		require.NoError(s.T(), err)

		resp, err := httpClient.Do(req)
		require.NoError(s.T(), err)
		defer close(s.T(), resp)

		assert.Equal(s.T(), http.StatusOK, resp.StatusCode)

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), body)

		mp := make(map[string]interface{})
		err = json.Unmarshal([]byte(body), &mp)
		require.NoError(s.T(), err)

		// Verify JSON response.
		alive := mp["alive"]
		require.IsType(s.T(), true, alive)
		require.True(s.T(), alive.(bool))

		environment := mp["environment"]
		require.IsType(s.T(), "", environment)
		require.Equal(s.T(), "e2e-tests", environment.(string))

		revision := mp["revision"]
		require.NotNil(s.T(), revision)

		buildTime := mp["buildTime"]
		require.NotNil(s.T(), buildTime)

		startTime := mp["startTime"]
		require.NotNil(s.T(), startTime)
	})
}

func (s *registrationServiceTestSuite) TestWoopra() {
	assertNotSecuredGetResponseEquals := func(endPointPath, expectedResponseValue string) {
		// Call woopra domain endpoint.
		req, err := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/%s", s.route, endPointPath), nil)
		require.NoError(s.T(), err)

		resp, err := httpClient.Do(req)
		require.NoError(s.T(), err)
		defer close(s.T(), resp)

		assert.Equal(s.T(), http.StatusOK, resp.StatusCode)

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), body)

		value := string(body)
		require.NoError(s.T(), err)

		// Verify JSON response.
		require.Equal(s.T(), expectedResponseValue, value)
	}

	s.Run("get woopra domain 200 OK", func() {
		// Call woopra domain endpoint.
		assertNotSecuredGetResponseEquals("woopra-domain", "test woopra domain")
	})

	s.Run("get segment write key 200 OK", func() {
		// Call segment write key endpoint.
		assertNotSecuredGetResponseEquals("segment-write-key", "test segment write key")
	})
}

func (s *registrationServiceTestSuite) TestAuthConfig() {
	s.Run("get authconfig 200 OK", func() {
		// Call authconfig endpoint.
		req, err := http.NewRequest("GET", s.route+"/api/v1/authconfig", nil)
		require.NoError(s.T(), err)

		resp, err := httpClient.Do(req)
		require.NoError(s.T(), err)
		defer close(s.T(), resp)

		assert.Equal(s.T(), http.StatusOK, resp.StatusCode)

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), body)
	})
}

func (s *registrationServiceTestSuite) TestSignupFails() {
	identity0 := authsupport.NewIdentity()
	emailClaim0 := authsupport.WithEmailClaim(uuid.NewV4().String() + "@acme.com")

	s.Run("post signup error no token 401 Unauthorized", func() {
		// Call signup endpoint without a token.
		requestBody, err := json.Marshal(map[string]string{})
		require.NoError(s.T(), err)
		req, err := http.NewRequest("POST", s.route+"/api/v1/signup", bytes.NewBuffer(requestBody))
		require.NoError(s.T(), err)
		req.Header.Set("content-type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(s.T(), err)
		defer close(s.T(), resp)

		// Retrieve unauthorized http status code.
		assert.Equal(s.T(), http.StatusUnauthorized, resp.StatusCode)

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), body)

		mp := make(map[string]interface{})
		err = json.Unmarshal([]byte(body), &mp)
		require.NoError(s.T(), err)

		// Check token error.
		tokenErr := mp["error"]
		require.Equal(s.T(), "no token found", tokenErr.(string))
	})
	s.Run("post signup error invalid token 401 Unauthorized", func() {
		// Call signup endpoint with an invalid token.
		mp := invokeEndpoint(s.T(), "POST", s.route+"/api/v1/signup", "1223123123", "", http.StatusUnauthorized)

		// Check token error.
		tokenErr := mp["error"]
		require.Equal(s.T(), "token contains an invalid number of segments", tokenErr.(string))
	})
	s.Run("post signup exp token 401 Unauthorized", func() {
		expClaim1 := authsupport.WithExpClaim(time.Now().Add(-60 * time.Second))

		// Not identical to the token used in POST signup - should return resource not found.
		token1, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0, expClaim1)
		require.NoError(s.T(), err)
		mp := invokeEndpoint(s.T(), "POST", s.route+"/api/v1/signup", token1, "", http.StatusUnauthorized)

		// Check token error.
		tokenErr := mp["error"]
		require.Contains(s.T(), tokenErr.(string), "token is expired by ")
	})
	s.Run("get signup error no token 401 Unauthorized", func() {
		// Call signup endpoint without a token.
		req, err := http.NewRequest("GET", s.route+"/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("content-type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(s.T(), err)
		defer close(s.T(), resp)

		// Retrieve unauthorized http status code.
		assert.Equal(s.T(), http.StatusUnauthorized, resp.StatusCode)

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), body)

		mp := make(map[string]interface{})
		err = json.Unmarshal([]byte(body), &mp)
		require.NoError(s.T(), err)

		// Check token error.
		tokenErr := mp["error"]
		require.Equal(s.T(), "no token found", tokenErr.(string))
	})
	s.Run("get signup error invalid token 401 Unauthorized", func() {
		// Call signup endpoint with an invalid token.
		mp := invokeEndpoint(s.T(), "GET", s.route+"/api/v1/signup", "1223123123", "", http.StatusUnauthorized)

		// Check token error.
		tokenErr := mp["error"]
		require.Equal(s.T(), "token contains an invalid number of segments", tokenErr.(string))
	})
	s.Run("get signup exp token 401 Unauthorized", func() {
		expClaim1 := authsupport.WithExpClaim(time.Now().Add(-60 * time.Second))

		// Not identical to the token used in POST signup - should return resource not found.
		token1, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0, expClaim1)
		require.NoError(s.T(), err)
		mp := invokeEndpoint(s.T(), "GET", s.route+"/api/v1/signup", token1, "", http.StatusUnauthorized)

		// Check token error.
		tokenErr := mp["error"]
		require.Contains(s.T(), tokenErr.(string), "token is expired by ")
	})
	s.Run("get signup 404 NotFound", func() {
		// Get valid generated token for e2e tests. IAT claim is overridden
		// to avoid token used before issued error.
		identity1 := authsupport.NewIdentity()
		emailClaim1 := authsupport.WithEmailClaim(uuid.NewV4().String() + "@acme.com")
		iatClaim1 := authsupport.WithIATClaim(time.Now().Add(-60 * time.Second))

		// Not identical to the token used in POST signup - should return resource not found.
		token1, err := authsupport.GenerateSignedE2ETestToken(*identity1, emailClaim1, iatClaim1)
		require.NoError(s.T(), err)

		// Call signup endpoint with a valid token.
		s.assertGetSignupReturnsNotFound(token1)
	})

	s.Run("get signup for crtadmin fails", func() {
		// Get valid generated token for e2e tests. IAT claim is overridden
		// to avoid token used before issued error. Username claim is also
		// overridden to trigger error and ensure that usersignup is not created.
		identity := authsupport.NewIdentity()
		emailValue := uuid.NewV4().String() + "@acme.com"
		emailClaim := authsupport.WithEmailClaim(emailValue)
		usernameClaim := authsupport.WithPreferredUsernameClaim("test-crtadmin")
		token, err := authsupport.GenerateSignedE2ETestToken(*identity, emailClaim, usernameClaim)
		require.NoError(s.T(), err)

		// Call signup endpoint with a valid token to initiate a signup process
		response := invokeEndpoint(s.T(), "POST", s.route+"/api/v1/signup", token, "", http.StatusForbidden)
		require.Equal(s.T(), "forbidden: failed to create usersignup for test-crtadmin", response["message"])
		require.Equal(s.T(), "error creating UserSignup resource", response["details"])
		require.Equal(s.T(), float64(403), response["code"])

		userSignup, err := s.hostAwait.WaitForUserSignup(identity.ID.String())
		require.Nil(s.T(), userSignup)
		require.Error(s.T(), err)
		require.EqualError(s.T(), err, "timed out waiting for the condition")
	})
}

func (s *registrationServiceTestSuite) TestSignupOK() {

	signupUser := func(token, email, userSignupName string, identity *authsupport.Identity) *v1alpha1.UserSignup {
		// Call signup endpoint with a valid token to initiate a signup process
		invokeEndpoint(s.T(), "POST", s.route+"/api/v1/signup", token, "", http.StatusAccepted)

		// Wait for the UserSignup to be created
		userSignup, err := s.hostAwait.WaitForUserSignup(userSignupName,
			wait.UntilUserSignupHasConditions(PendingApproval()...),
			wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValuePending))
		require.NoError(s.T(), err)
		emailAnnotation := userSignup.Annotations[v1alpha1.UserSignupUserEmailAnnotationKey]
		assert.Equal(s.T(), email, emailAnnotation)

		// Call get signup endpoint with a valid token and make sure it's pending approval
		s.assertGetSignupStatusPendingApproval(identity.Username, token)

		// Attempt to create same usersignup by calling post signup with same token should return an error
		mp := invokeEndpoint(s.T(), "POST", s.route+"/api/v1/signup", token, "", http.StatusConflict)
		assert.Equal(s.T(), fmt.Sprintf("Operation cannot be fulfilled on  \"\": UserSignup [id: %s; username: %s]. Unable to create UserSignup because there is already an active UserSignup with such ID",
			userSignupName, identity.Username), mp["message"])
		assert.Equal(s.T(), "error creating UserSignup resource", mp["details"])

		// Approve usersignup.
		userSignup.Spec.Approved = true
		userSignup.Spec.TargetCluster = s.memberAwait.ClusterName
		err = s.hostAwait.Client.Update(context.TODO(), userSignup)
		require.NoError(s.T(), err)

		// Wait the Master User Record to be provisioned
		VerifyResourcesProvisionedForSignup(s.T(), s.hostAwait, *userSignup, "basic", s.memberAwait)

		// Call signup endpoint with same valid token to check if status changed to Provisioned now
		s.assertGetSignupStatusProvisioned(identity.Username, token)

		return userSignup
	}

	s.Run("test activation-deactivation workflow", func() {
		// Get valid generated token for e2e tests. IAT claim is overridden
		// to avoid token used before issued error.
		identity := authsupport.NewIdentity()
		emailValue := uuid.NewV4().String() + "@acme.com"
		emailClaim := authsupport.WithEmailClaim(emailValue)
		t, err := authsupport.GenerateSignedE2ETestToken(*identity, emailClaim)
		require.NoError(s.T(), err)

		// Signup a new user
		userSignup := signupUser(t, emailValue, identity.ID.String(), identity)

		// Deactivate the usersignup
		userSignup, err = s.hostAwait.UpdateUserSignupSpec(userSignup.Name, func(us *v1alpha1.UserSignup) {
			us.Spec.Deactivated = true
		})
		require.NoError(s.T(), err)
		userSignup, err = s.hostAwait.WaitForUserSignup(userSignup.Name,
			wait.UntilUserSignupHasConditions(Deactivated()...),
			wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueDeactivated))
		require.NoError(s.T(), err)

		// Now check that the reg-service treats the deactivated usersignup as nonexistent and returns 404
		s.assertGetSignupReturnsNotFound(t)

		// Re-activate the usersignup by calling the signup endpoint with the same token/user again
		userSignup = signupUser(t, emailValue, identity.ID.String(), identity)
	})

	s.Run("test User ID encodings", func() {
		userIDs := []string{
			"abcde-12345",
			"abcde\\*-12345",
			"-1234567",
			"0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-01234567890123456789",
			//"abc:xyz",
		}

		encodedUserIDs := []string{
			"abcde-12345",
			"c0177ca4-abcde-12345",
			"ca3e1e0f-1234567",
			"e3632025-0123456789abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqr",
			//"a05a4053-abcxyz",
		}

		for i, userID := range userIDs {
			identity := authsupport.NewIdentity()
			emailValue := uuid.NewV4().String() + "@acme.com"
			emailClaim := authsupport.WithEmailClaim(emailValue)
			t, err := authsupport.GenerateSignedE2ETestToken(*identity, emailClaim, authsupport.WithSubClaim(userID))
			require.NoError(s.T(), err)

			// Signup a new user
			userSignup := signupUser(t, emailValue, encodedUserIDs[i], identity)

			require.Equal(s.T(), userID, userSignup.Spec.UserID)
		}
	})
}

func (s *registrationServiceTestSuite) TestPhoneVerification() {
	// Create a token and identity to sign up with
	identity0 := authsupport.NewIdentity()
	emailValue := uuid.NewV4().String() + "@some.domain"
	emailClaim0 := authsupport.WithEmailClaim(emailValue)
	token0, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0)
	require.NoError(s.T(), err)

	// Call the signup endpoint
	invokeEndpoint(s.T(), "POST", s.route+"/api/v1/signup", token0, "", http.StatusAccepted)

	// Wait for the UserSignup to be created
	userSignup, err := s.hostAwait.WaitForUserSignup(identity0.ID.String(),
		wait.UntilUserSignupHasConditions(VerificationRequired()...),
		wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(s.T(), err)
	emailAnnotation := userSignup.Annotations[v1alpha1.UserSignupUserEmailAnnotationKey]
	assert.Equal(s.T(), emailValue, emailAnnotation)

	// Call get signup endpoint with a valid token and make sure verificationRequired is true
	mp, mpStatus := parseResponse(s.T(), invokeEndpoint(s.T(), "GET", s.route+"/api/v1/signup", token0, "", http.StatusOK))
	assert.Equal(s.T(), "", mp["compliantUsername"])
	assert.Equal(s.T(), identity0.Username, mp["username"])
	require.IsType(s.T(), false, mpStatus["ready"])
	assert.False(s.T(), mpStatus["ready"].(bool))
	assert.Equal(s.T(), "PendingApproval", mpStatus["reason"])
	require.True(s.T(), mpStatus["verificationRequired"].(bool))

	// Confirm the status of the UserSignup is correct
	_, err = s.hostAwait.WaitForUserSignup(identity0.ID.String(),
		wait.UntilUserSignupHasConditions(VerificationRequired()...),
		wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(s.T(), err)

	// Confirm that a MUR hasn't been created
	obj := &v1alpha1.MasterUserRecord{}
	err = s.hostAwait.Client.Get(context.TODO(), types.NamespacedName{Namespace: s.hostAwait.Namespace, Name: identity0.Username}, obj)
	require.Error(s.T(), err)
	require.True(s.T(), errors.IsNotFound(err))

	// Initiate the verification process
	invokeEndpoint(s.T(), "PUT", s.route+"/api/v1/signup/verification", token0,
		`{ "country_code":"+61", "phone_number":"408999999" }`, http.StatusNoContent)

	// Retrieve the updated UserSignup
	userSignup, err = s.hostAwait.WaitForUserSignup(identity0.ID.String())
	require.NoError(s.T(), err)

	// Confirm there is a verification code annotation value, and store it in a variable
	verificationCode := userSignup.Annotations[v1alpha1.UserSignupVerificationCodeAnnotationKey]
	require.NotEmpty(s.T(), verificationCode)

	// Confirm the expiry time has been set
	require.NotEmpty(s.T(), userSignup.Annotations[v1alpha1.UserVerificationExpiryAnnotationKey])

	// Attempt to verify with an incorrect verification code
	invokeEndpoint(s.T(), "GET", s.route+"/api/v1/signup/verification/invalid", token0, "", http.StatusForbidden)

	// Retrieve the updated UserSignup
	userSignup, err = s.hostAwait.WaitForUserSignup(identity0.ID.String())
	require.NoError(s.T(), err)

	// Check attempts has been incremented
	require.NotEmpty(s.T(), userSignup.Annotations[v1alpha1.UserVerificationAttemptsAnnotationKey])

	// Confirm the verification code has not changed
	require.Equal(s.T(), verificationCode, userSignup.Annotations[v1alpha1.UserSignupVerificationCodeAnnotationKey])

	// Verify with the correct code
	invokeEndpoint(s.T(), "GET", s.route+fmt.Sprintf("/api/v1/signup/verification/%s",
		userSignup.Annotations[v1alpha1.UserSignupVerificationCodeAnnotationKey]), token0, "", http.StatusOK)

	// Retrieve the updated UserSignup
	userSignup, err = s.hostAwait.WaitForUserSignup(identity0.ID.String(),
		wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValuePending))
	require.NoError(s.T(), err)

	// Confirm all unrequired verification-related annotations have been removed
	require.Empty(s.T(), userSignup.Annotations[v1alpha1.UserVerificationExpiryAnnotationKey])
	require.Empty(s.T(), userSignup.Annotations[v1alpha1.UserVerificationAttemptsAnnotationKey])
	require.Empty(s.T(), userSignup.Annotations[v1alpha1.UserSignupVerificationCodeAnnotationKey])
	require.Empty(s.T(), userSignup.Annotations[v1alpha1.UserSignupVerificationTimestampAnnotationKey])
	require.Empty(s.T(), userSignup.Annotations[v1alpha1.UserSignupVerificationCounterAnnotationKey])
	require.Empty(s.T(), userSignup.Annotations[v1alpha1.UserSignupVerificationInitTimestampAnnotationKey])

	// Call get signup endpoint with a valid token and make sure it's pending approval
	mp, mpStatus = parseResponse(s.T(), invokeEndpoint(s.T(), "GET", s.route+"/api/v1/signup", token0, "", http.StatusOK))
	assert.Equal(s.T(), "", mp["compliantUsername"])
	assert.Equal(s.T(), identity0.Username, mp["username"])
	require.IsType(s.T(), false, mpStatus["ready"])
	assert.False(s.T(), mpStatus["ready"].(bool))
	assert.Equal(s.T(), "PendingApproval", mpStatus["reason"])
	require.False(s.T(), mpStatus["verificationRequired"].(bool))

	// Now approve the usersignup.
	userSignup.Spec.Approved = true
	err = s.hostAwait.Client.Update(context.TODO(), userSignup)
	require.NoError(s.T(), err)

	// Confirm the MasterUserRecord is provisioned
	_, err = s.hostAwait.WaitForMasterUserRecord(identity0.Username, wait.UntilMasterUserRecordHasCondition(Provisioned()))
	require.NoError(s.T(), err)

	// Retrieve the UserSignup from the GET endpoint
	_, mpStatus = parseResponse(s.T(), invokeEndpoint(s.T(), "GET", s.route+"/api/v1/signup", token0, "", http.StatusOK))

	// Confirm that VerificationRequired is no longer true
	require.False(s.T(), mpStatus["verificationRequired"].(bool))

	// Create another token and identity to sign up with
	otherIdentity := authsupport.NewIdentity()
	otherEmailValue := uuid.NewV4().String() + "@other.domain"
	otherEmailClaim := authsupport.WithEmailClaim(otherEmailValue)
	otherToken, err := authsupport.GenerateSignedE2ETestToken(*otherIdentity, otherEmailClaim)
	require.NoError(s.T(), err)

	// Call the signup endpoint
	invokeEndpoint(s.T(), "POST", s.route+"/api/v1/signup", otherToken, "", http.StatusAccepted)

	// Wait for the UserSignup to be created
	otherUserSignup, err := s.hostAwait.WaitForUserSignup(otherIdentity.ID.String(),
		wait.UntilUserSignupHasConditions(VerificationRequired()...),
		wait.UntilUserSignupHasStateLabel(v1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(s.T(), err)
	otherEmailAnnotation := otherUserSignup.Annotations[v1alpha1.UserSignupUserEmailAnnotationKey]
	assert.Equal(s.T(), otherEmailValue, otherEmailAnnotation)

	// Initiate the verification process using the same phone number as previously
	responseMap := invokeEndpoint(s.T(), "PUT", s.route+"/api/v1/signup/verification", otherToken,
		`{ "country_code":"+61", "phone_number":"408999999" }`, http.StatusForbidden)

	require.NotEmpty(s.T(), responseMap)
	require.Equal(s.T(), float64(http.StatusForbidden), responseMap["code"], "code not found in response body map %s", responseMap)

	require.Equal(s.T(), "Forbidden", responseMap["status"])
	require.Equal(s.T(), "forbidden: phone number already in use: cannot register using phone number: +61408999999", responseMap["message"])
	require.Equal(s.T(), "error while initiating verification", responseMap["details"])

	// Retrieve the updated UserSignup
	otherUserSignup, err = s.hostAwait.WaitForUserSignup(otherIdentity.ID.String())
	require.NoError(s.T(), err)

	// Confirm there is no verification code annotation value
	require.Empty(s.T(), otherUserSignup.Annotations[v1alpha1.UserSignupVerificationCodeAnnotationKey])

	// Retrieve the current UserSignup
	err = s.hostAwait.Client.Get(context.TODO(), types.NamespacedName{Namespace: s.hostAwait.Namespace, Name: userSignup.Name}, userSignup)
	require.NoError(s.T(), err)

	// Now mark the original UserSignup as deactivated
	userSignup.Spec.Deactivated = true

	// Update the UserSignup
	err = s.hostAwait.Client.Update(context.TODO(), userSignup)
	require.NoError(s.T(), err)

	// Now attempt the verification again
	invokeEndpoint(s.T(), "PUT", s.route+"/api/v1/signup/verification", otherToken,
		`{ "country_code":"+61", "phone_number":"408999999" }`, http.StatusNoContent)

	// Retrieve the updated UserSignup again
	otherUserSignup, err = s.hostAwait.WaitForUserSignup(otherIdentity.ID.String())
	require.NoError(s.T(), err)

	// Confirm there is now a verification code annotation value
	require.NotEmpty(s.T(), otherUserSignup.Annotations[v1alpha1.UserSignupVerificationCodeAnnotationKey])
}

func (s *registrationServiceTestSuite) assertGetSignupStatusProvisioned(username, bearerToken string) {
	mp, mpStatus := parseResponse(s.T(), invokeEndpoint(s.T(), "GET", s.route+"/api/v1/signup", bearerToken, "", http.StatusOK))
	assert.Equal(s.T(), username, mp["compliantUsername"])
	assert.Equal(s.T(), username, mp["username"])
	require.IsType(s.T(), false, mpStatus["ready"])
	assert.True(s.T(), mpStatus["ready"].(bool))
	assert.Equal(s.T(), "Provisioned", mpStatus["reason"])
	assert.Equal(s.T(), s.memberAwait.GetConsoleURL(), mp["consoleURL"])
}

func (s *registrationServiceTestSuite) assertGetSignupStatusPendingApproval(username, bearerToken string) {
	mp, mpStatus := parseResponse(s.T(), invokeEndpoint(s.T(), "GET", s.route+"/api/v1/signup", bearerToken, "", http.StatusOK))
	assert.Equal(s.T(), username, mp["username"])
	require.IsType(s.T(), false, mpStatus["ready"])
	assert.False(s.T(), mpStatus["ready"].(bool))
	assert.Equal(s.T(), "PendingApproval", mpStatus["reason"])
}

func (s *registrationServiceTestSuite) assertGetSignupReturnsNotFound(bearerToken string) {
	invokeEndpoint(s.T(), "GET", s.route+"/api/v1/signup", bearerToken, "", http.StatusNotFound)
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
	resp, err := httpClient.Do(req)
	require.NoError(t, err)

	defer close(t, resp)

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

func close(t *testing.T, resp *http.Response) {
	_, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	err = resp.Body.Close()
	require.NoError(t, err)
}
