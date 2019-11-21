package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	framework "github.com/operator-framework/operator-sdk/pkg/test"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestRegistrationService(t *testing.T) {
	suite.Run(t, &registrationServiceTestSuite{})
}

type registrationServiceTestSuite struct {
	suite.Suite
	namespace  string
	route      string
	testCtx    *framework.TestCtx
	awaitility *wait.Awaitility
}

func (s *registrationServiceTestSuite) SetupSuite() {
	userSignupList := &v1alpha1.UserSignupList{}
	s.testCtx, s.awaitility = testsupport.WaitForDeployments(s.T(), userSignupList)
	s.namespace = s.awaitility.RegistrationServiceNs
	s.route = s.awaitility.RegistrationServiceURL
}

func (s *registrationServiceTestSuite) TestLandingPageReachable() {
	// just make sure that the landing page is reachable
	req, err := http.NewRequest("GET", s.route, nil)
	require.NoError(s.T(), err)
	client := getClient()

    resp, err := client.Do(req)

    defer close(s.T(), resp)

	require.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusOK, resp.StatusCode)
}

func (s *registrationServiceTestSuite) TestHealth() {
	s.Run("get healthcheck 200 OK", func() {
		// Call health endpoint.
		req, err := http.NewRequest("GET", s.route+"/api/v1/health", nil)
		require.NoError(s.T(), err)
		client := getClient()

		resp, err := client.Do(req)
		require.NoError(s.T(), err)
		assert.Equal(s.T(), http.StatusOK, resp.StatusCode)

		defer close(s.T(), resp)

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
		client := getClient()

		resp, err := client.Do(req)
		require.NoError(s.T(), err)
		assert.Equal(s.T(), http.StatusOK, resp.StatusCode)

		defer close(s.T(), resp)

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), body)
	})
}
func (s *registrationServiceTestSuite) TestSignupFails() {
	identity0 := authsupport.NewIdentity()
	emailClaim0 := authsupport.WithEmailClaim(uuid.NewV4().String() + "@email.tld")
	iatClaim0 := authsupport.WithIATClaim(time.Now().Add(-60 * time.Second))

	s.Run("post signup error no token 401 Unauthorized", func() {
		// Call signup endpoint without a token.
		requestBody, err := json.Marshal(map[string]string{})
		require.NoError(s.T(), err)
		req, err := http.NewRequest("POST", s.route+"/api/v1/signup", bytes.NewBuffer(requestBody))
		require.NoError(s.T(), err)
		req.Header.Set("content-type", "application/json")
		client := getClient()

		resp, err := client.Do(req)
		require.NoError(s.T(), err)

		// Retrieve unauthorized http status code.
		assert.Equal(s.T(), http.StatusUnauthorized, resp.StatusCode)

		defer close(s.T(), resp)

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
		client := getClient()

		resp, err := client.Do(req)
		require.NoError(s.T(), err)

		// Retrieve unauthorized http status code.
		assert.Equal(s.T(), http.StatusUnauthorized, resp.StatusCode)

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
		require.Equal(s.T(), "token contains an invalid number of segments", tokenErr.(string))
	})
	s.Run("post signup exp token 401 Unauthorized", func() {
		expClaim1 := authsupport.WithExpClaim(time.Now().Add(-60 * time.Second))

		// Not identical to the token used in POST signup - should return resource not found.
		token1, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0, iatClaim0, expClaim1)
		require.NoError(s.T(), err)

		// Call signup endpoint with an valid token.
		req, err := http.NewRequest("POST", s.route+"/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("Authorization", "Bearer "+token1)
		req.Header.Set("content-type", "application/json")
		client := getClient()

		resp, err := client.Do(req)
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
		client := getClient()

		resp, err := client.Do(req)
		require.NoError(s.T(), err)

		// Retrieve unauthorized http status code.
		assert.Equal(s.T(), http.StatusUnauthorized, resp.StatusCode)

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
		require.Equal(s.T(), "no token found", tokenErr.(string))
	})
	s.Run("get signup error invalid token 401 Unauthorized", func() {
		// Call signup endpoint with an invalid token.
		req, err := http.NewRequest("GET", s.route+"/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("Authorization", "Bearer 1223123123")
		req.Header.Set("content-type", "application/json")
		client := getClient()

		resp, err := client.Do(req)
		require.NoError(s.T(), err)

		// Retrieve unauthorized http status code.
		assert.Equal(s.T(), http.StatusUnauthorized, resp.StatusCode)

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
		require.Equal(s.T(), "token contains an invalid number of segments", tokenErr.(string))
	})
	s.Run("get signup exp token 401 Unauthorized", func() {
		expClaim1 := authsupport.WithExpClaim(time.Now().Add(-60 * time.Second))

		// Not identical to the token used in POST signup - should return resource not found.
		token1, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0, iatClaim0, expClaim1)
		require.NoError(s.T(), err)

		// Call signup endpoint with an valid token.
		req, err := http.NewRequest("GET", s.route+"/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("Authorization", "Bearer "+token1)
		req.Header.Set("content-type", "application/json")
		client := getClient()

		resp, err := client.Do(req)
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
		emailClaim1 := authsupport.WithEmailClaim(uuid.NewV4().String() + "@email.tld")
		iatClaim1 := authsupport.WithIATClaim(time.Now().Add(-60 * time.Second))

		// Not identical to the token used in POST signup - should return resource not found.
		token1, err := authsupport.GenerateSignedE2ETestToken(*identity1, emailClaim1, iatClaim1)
		require.NoError(s.T(), err)

		// Call signup endpoint with an valid token.
		req, err := http.NewRequest("GET", s.route+"/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("Authorization", "Bearer "+token1)
		req.Header.Set("content-type", "application/json")
		client := getClient()

		resp, err := client.Do(req)
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
	emailClaim0 := authsupport.WithEmailClaim(uuid.NewV4().String() + "@email.tld")
	iatClaim0 := authsupport.WithIATClaim(time.Now().Add(-60 * time.Second))
	token0, err := authsupport.GenerateSignedE2ETestToken(*identity0, emailClaim0, iatClaim0)
	require.NoError(s.T(), err)

	// Call signup endpoint with an valid token.
	req, err := http.NewRequest("POST", s.route+"/api/v1/signup", nil)
	require.NoError(s.T(), err)
	req.Header.Set("Authorization", "Bearer "+token0)
	req.Header.Set("content-type", "application/json")
	client := getClient()

	resp, err := client.Do(req)
	require.NoError(s.T(), err)

	defer close(s.T(), resp)

	body, err := ioutil.ReadAll(resp.Body)
	require.NoError(s.T(), err)
	require.NotNil(s.T(), body)
	assert.Equal(s.T(), http.StatusAccepted, resp.StatusCode)

	// Attempt to create same usersignup by calling post signup with same token
	resp, err = client.Do(req)
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

	// Call get signup endpoint with an valid token.
	req, err = http.NewRequest("GET", s.route+"/api/v1/signup", nil)
	require.NoError(s.T(), err)
	req.Header.Set("Authorization", "Bearer "+token0)
	req.Header.Set("content-type", "application/json")

	resp, err = client.Do(req)
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

	assert.Nil(s.T(), mp["CompliantUsername"])
	assert.Equal(s.T(), identity0.Username, mp["username"])
	require.IsType(s.T(), false, mpStatus["ready"])
	assert.False(s.T(), mpStatus["ready"].(bool))
	assert.Equal(s.T(), "PendingApproval", mpStatus["reason"])

	userSignup, err := s.awaitility.Host().WaitForUserSignup(identity0.ID.String(), wait.UntilUserSignupHasConditions(pendingApproval()...))
	require.NoError(s.T(), err)

	// Approve usersignup.
	userSignup.Spec.Approved = true
	err = s.awaitility.Host().Client.Update(context.TODO(), userSignup)
	require.NoError(s.T(), err)

	_, err = s.awaitility.Host().WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(approvedByAdmin()...))
	require.NoError(s.T(), err)

	// Call signup endpoint with same valid token to check if status changed.
	resp, err = client.Do(req)
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

	assert.Nil(s.T(), mp["CompliantUsername"])
	assert.Equal(s.T(), identity0.Username, mp["username"])
	require.IsType(s.T(), false, mpStatus["ready"])
	assert.False(s.T(), mpStatus["ready"].(bool))
	assert.Equal(s.T(), "Provisioning", mpStatus["reason"])
}

// getClient create's a new client.
func getClient() *http.Client {
	return &http.Client{
		Timeout: time.Second * 10,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
}

func close(t *testing.T, resp *http.Response) {
    _, err := ioutil.ReadAll(resp.Body)
    require.NoError(t, err)
    err = resp.Body.Close()
    require.NoError(t, err)
}