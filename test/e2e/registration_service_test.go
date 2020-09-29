package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
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
	s.ctx, s.hostAwait, s.memberAwait = WaitForDeployments(s.T(), userSignupList)
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
		require.IsType(s.T(), "", tokenErr)
		require.Equal(s.T(), "no token found", tokenErr.(string))
	})
	s.Run("post signup error invalid token 401 Unauthorized", func() {
		// Call signup endpoint with an invalid token.
		req, err := http.NewRequest("POST", s.route+"/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("Authorization", "Bearer 1223123123")
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
		require.IsType(s.T(), tokenErr, "")
		require.Equal(s.T(), "token contains an invalid number of segments", tokenErr.(string))
	})
	s.Run("post signup exp token 401 Unauthorized", func() {
		expClaim1 := authsupport.WithExpClaim(time.Now().Add(-60 * time.Second))

		// Not identical to the token used in POST signup - should return resource not found.
		token1, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0, expClaim1)
		require.NoError(s.T(), err)

		// Call signup endpoint with a valid token.
		req, err := http.NewRequest("POST", s.route+"/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("Authorization", "Bearer "+token1)
		req.Header.Set("content-type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(s.T(), err)
		defer close(s.T(), resp)

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), body)

		mp := make(map[string]interface{})
		err = json.Unmarshal([]byte(body), &mp)
		require.NoError(s.T(), err)

		// Check token error.
		tokenErr := mp["error"]
		require.IsType(s.T(), tokenErr, "")
		require.Contains(s.T(), tokenErr.(string), "token is expired by ")

		assert.Equal(s.T(), http.StatusUnauthorized, resp.StatusCode)
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
		require.IsType(s.T(), tokenErr, "")
		require.Equal(s.T(), "no token found", tokenErr.(string))
	})
	s.Run("get signup error invalid token 401 Unauthorized", func() {
		// Call signup endpoint with an invalid token.
		req, err := http.NewRequest("GET", s.route+"/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("Authorization", "Bearer 1223123123")
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
		require.IsType(s.T(), tokenErr, "")
		require.Equal(s.T(), "token contains an invalid number of segments", tokenErr.(string))
	})
	s.Run("get signup exp token 401 Unauthorized", func() {
		expClaim1 := authsupport.WithExpClaim(time.Now().Add(-60 * time.Second))

		// Not identical to the token used in POST signup - should return resource not found.
		token1, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0, expClaim1)
		require.NoError(s.T(), err)

		// Call signup endpoint with a valid token.
		req, err := http.NewRequest("GET", s.route+"/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("Authorization", "Bearer "+token1)
		req.Header.Set("content-type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(s.T(), err)
		defer close(s.T(), resp)

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), body)

		mp := make(map[string]interface{})
		err = json.Unmarshal([]byte(body), &mp)
		require.NoError(s.T(), err)

		// Check token error.
		tokenErr := mp["error"]
		require.IsType(s.T(), tokenErr, "")
		require.Contains(s.T(), tokenErr.(string), "token is expired by ")

		assert.Equal(s.T(), http.StatusUnauthorized, resp.StatusCode)
	})
	s.Run("get signup 404 NotFound", func() {
		// Get valid generated token for e2e tests. IAT claim is overriden
		// to avoid token used before issued error.
		identity1 := authsupport.NewIdentity()
		emailClaim1 := authsupport.WithEmailClaim(uuid.NewV4().String() + "@acme.com")
		iatClaim1 := authsupport.WithIATClaim(time.Now().Add(-60 * time.Second))

		// Not identical to the token used in POST signup - should return resource not found.
		token1, err := authsupport.GenerateSignedE2ETestToken(*identity1, emailClaim1, iatClaim1)
		require.NoError(s.T(), err)

		// Call signup endpoint with a valid token.
		req, err := http.NewRequest("GET", s.route+"/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("Authorization", "Bearer "+token1)
		req.Header.Set("content-type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(s.T(), err)
		defer close(s.T(), resp)

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), body)

		assert.Equal(s.T(), http.StatusNotFound, resp.StatusCode)
	})
}

func (s *registrationServiceTestSuite) TestSignupOK() {
	// Get valid generated token for e2e tests. IAT claim is overriden
	// to avoid token used before issued error.
	identity0 := authsupport.NewIdentity()
	emailValue := uuid.NewV4().String() + "@acme.com"
	emailClaim0 := authsupport.WithEmailClaim(emailValue)
	token0, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0)
	require.NoError(s.T(), err)

	// Call signup endpoint with a valid token to initiate a signup process
	req, err := http.NewRequest("POST", s.route+"/api/v1/signup", nil)
	require.NoError(s.T(), err)
	req.Header.Set("Authorization", "Bearer "+token0)
	req.Header.Set("content-type", "application/json")

	resp, err := httpClient.Do(req)
	require.NoError(s.T(), err)
	defer close(s.T(), resp)

	body, err := ioutil.ReadAll(resp.Body)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), body)
	assert.Equal(s.T(), http.StatusAccepted, resp.StatusCode)

	// Attempt to create same usersignup by calling post signup with same token should return an error
	resp, err = httpClient.Do(req)
	require.NoError(s.T(), err)
	defer close(s.T(), resp)

	body, err = ioutil.ReadAll(resp.Body)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), body)

	mp := make(map[string]interface{})
	err = json.Unmarshal([]byte(body), &mp)
	require.NoError(s.T(), err)

	assert.Equal(s.T(), "usersignups.toolchain.dev.openshift.com \""+identity0.ID.String()+"\" already exists", mp["message"])
	assert.Equal(s.T(), "error creating UserSignup resource", mp["details"])
	assert.Equal(s.T(), http.StatusInternalServerError, resp.StatusCode)

	// Wait for the UserSignup to be created
	userSignup, err := s.hostAwait.WaitForUserSignup(identity0.ID.String(), wait.UntilUserSignupHasConditions(PendingApproval()...))
	require.NoError(s.T(), err)
	emailAnnotation := userSignup.Annotations[v1alpha1.UserSignupUserEmailAnnotationKey]
	assert.Equal(s.T(), emailValue, emailAnnotation)

	// Call get signup endpoint with a valid token and make sure it's pending approval
	req, err = http.NewRequest("GET", s.route+"/api/v1/signup", nil)
	require.NoError(s.T(), err)
	req.Header.Set("Authorization", "Bearer "+token0)
	req.Header.Set("content-type", "application/json")

	resp, err = httpClient.Do(req)
	require.NoError(s.T(), err)
	defer close(s.T(), resp)

	body, err = ioutil.ReadAll(resp.Body)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), body)

	mp = make(map[string]interface{})
	err = json.Unmarshal([]byte(body), &mp)
	require.NoError(s.T(), err)

	mpStatus, ok := mp["status"].(map[string]interface{})
	assert.True(s.T(), ok)

	assert.Equal(s.T(), "", mp["compliantUsername"])
	assert.Equal(s.T(), identity0.Username, mp["username"])
	require.IsType(s.T(), false, mpStatus["ready"])
	assert.False(s.T(), mpStatus["ready"].(bool))
	assert.Equal(s.T(), "PendingApproval", mpStatus["reason"])

	// Approve usersignup.
	userSignup.Spec.Approved = true
	err = s.hostAwait.Client.Update(context.TODO(), userSignup)
	require.NoError(s.T(), err)

	// Wait the Master User Record to be provisioned
	_, err = s.hostAwait.WaitForMasterUserRecord(identity0.Username, wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()))
	require.NoError(s.T(), err)

	// Call signup endpoint with same valid token to check if status changed to Provisioned now
	resp, err = httpClient.Do(req)
	require.NoError(s.T(), err)
	defer close(s.T(), resp)

	body, err = ioutil.ReadAll(resp.Body)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), body)

	mp = make(map[string]interface{})
	err = json.Unmarshal([]byte(body), &mp)
	require.NoError(s.T(), err)

	mpStatus, ok = mp["status"].(map[string]interface{})
	assert.True(s.T(), ok)

	assert.Equal(s.T(), identity0.Username, mp["compliantUsername"])
	assert.Equal(s.T(), identity0.Username, mp["username"])
	require.IsType(s.T(), true, mpStatus["ready"])
	assert.True(s.T(), mpStatus["ready"].(bool))
	assert.Equal(s.T(), "Provisioned", mpStatus["reason"])

	// Verify console URL
	memberCluster, ok, err := s.hostAwait.GetToolchainCluster(cluster.Member, s.memberAwait.Namespace, nil)
	require.NoError(s.T(), err)
	require.True(s.T(), ok)
	assert.Equal(s.T(), ExpectedConsoleURL(s.T(), s.memberAwait, memberCluster), mp["consoleURL"])
}

func (s *registrationServiceTestSuite) TestPhoneVerification() {
	// Create a token and identity to sign up with
	identity0 := authsupport.NewIdentity()
	emailValue := uuid.NewV4().String() + "@some.domain"
	emailClaim0 := authsupport.WithEmailClaim(emailValue)
	token0, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0)
	require.NoError(s.T(), err)

	// Call the signup endpoint
	invokeEndpoint(s.T(), "POST", s.route + "/api/v1/signup", token0, "", http.StatusAccepted)

	// Wait for the UserSignup to be created
	userSignup, err := s.hostAwait.WaitForUserSignup(identity0.ID.String(), wait.UntilUserSignupHasConditions(PendingApproval()...))
	require.NoError(s.T(), err)
	emailAnnotation := userSignup.Annotations[v1alpha1.UserSignupUserEmailAnnotationKey]
	assert.Equal(s.T(), emailValue, emailAnnotation)

	// Call get signup endpoint with a valid token and make sure it's pending approval
	body := invokeEndpoint(s.T(), "GET", s.route + "/api/v1/signup", token0, "", http.StatusOK)

	// Convert the signup response into a map so that we can examine its values
	mp := make(map[string]interface{})
	err = json.Unmarshal([]byte(body), &mp)
	require.NoError(s.T(), err)

	// Check that the response looks fine
	mpStatus, ok := mp["status"].(map[string]interface{})
	assert.True(s.T(), ok)
	assert.Equal(s.T(), "", mp["compliantUsername"])
	assert.Equal(s.T(), identity0.Username, mp["username"])
	require.IsType(s.T(), false, mpStatus["ready"])
	assert.False(s.T(), mpStatus["ready"].(bool))
	assert.Equal(s.T(), "PendingApproval", mpStatus["reason"])
	require.True(s.T(), mpStatus["verificationRequired"].(bool))

	// Now approve the usersignup.
	userSignup.Spec.Approved = true
	err = s.hostAwait.Client.Update(context.TODO(), userSignup)
	require.NoError(s.T(), err)

	// Ensure the Master User Record is NOT provisioned
	err = s.hostAwait.WaitUntilMasterUserRecordDeleted(identity0.Username)
	require.NoError(s.T(), err)

	// Initiate the verification process
	invokeEndpoint(s.T(), "PUT", s.route + "/api/v1/signup/verification", token0,
		`{ "country_code":"+61", "phone_number":"408999999" }`, http.StatusNoContent)

	// Retrieve the updated UserSignup
	userSignup, err = s.hostAwait.WaitForUserSignup(identity0.ID.String())
	require.NoError(s.T(), err)

	// Confirm there is a verification code label
	require.NotEmpty(s.T(), userSignup.Annotations[v1alpha1.UserSignupVerificationCodeAnnotationKey])

	// Confirm the expiry time has been set
	require.NotEmpty(s.T(), userSignup.Annotations[v1alpha1.UserVerificationExpiryAnnotationKey])

	// Attempt to verify with an incorrect verification code
	invokeEndpoint(s.T(), "GET", s.route+"/api/v1/signup/verification/invalid", token0, "", http.StatusForbidden)

	// Retrieve the updated UserSignup
	userSignup, err = s.hostAwait.WaitForUserSignup(identity0.ID.String())
	require.NoError(s.T(), err)

	// Check attempts has been incremented
	require.NotEmpty(s.T(), userSignup.Annotations[v1alpha1.UserVerificationAttemptsAnnotationKey])

	// Verify with the correct code
	invokeEndpoint(s.T(), "GET", s.route + fmt.Sprintf("/api/v1/signup/verification/%s",
		userSignup.Annotations[v1alpha1.UserSignupVerificationCodeAnnotationKey]), token0, "", http.StatusOK)

	// Retrieve the updated UserSignup
	userSignup, err = s.hostAwait.WaitForUserSignup(identity0.ID.String())
	require.NoError(s.T(), err)

	// Confirm all unrequired verification-related annotations have been removed
	require.Empty(s.T(), userSignup.Annotations[v1alpha1.UserVerificationExpiryAnnotationKey])
	require.Empty(s.T(), userSignup.Annotations[v1alpha1.UserVerificationAttemptsAnnotationKey])
	require.Empty(s.T(), userSignup.Annotations[v1alpha1.UserSignupVerificationCodeAnnotationKey])
	require.Empty(s.T(), userSignup.Annotations[v1alpha1.UserSignupVerificationTimestampAnnotationKey])
	require.Empty(s.T(), userSignup.Annotations[v1alpha1.UserSignupVerificationCounterAnnotationKey])
	require.Empty(s.T(), userSignup.Annotations[v1alpha1.UserSignupVerificationInitTimestampAnnotationKey])

	// Confirm the MasterUserRecord is provisioned
	_, err = s.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
	require.NoError(s.T(), err)

	// Retrieve the UserSignup from the GET endpoint
	body = invokeEndpoint(s.T(), "GET", s.route+"/api/v1/signup", token0, "", http.StatusOK)

	// Parse the body of the response into a map
	mp = make(map[string]interface{})
	err = json.Unmarshal([]byte(body), &mp)
	require.NoError(s.T(), err)

	mpStatus, ok = mp["status"].(map[string]interface{})
	assert.True(s.T(), ok)

	// Confirm that VerificationRequired is no longer true
	require.False(s.T(), mpStatus["verificationRequired"].(bool))
}

func invokeEndpoint(t *testing.T, method, path, authToken, requestBody string, requiredStatus int) ([]byte) {
	req, err := http.NewRequest(method, path, strings.NewReader(requestBody))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("content-type", "application/json")
	resp, err := httpClient.Do(req)
	require.NoError(t, err)

	defer close(t, resp)

	body, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NotNil(t, body)
	require.Equal(t, requiredStatus, resp.StatusCode)

	return body
}

func close(t *testing.T, resp *http.Response) {
	_, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	err = resp.Body.Close()
	require.NoError(t, err)
}
