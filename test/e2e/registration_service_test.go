package e2e

import (
	//"os"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"testing"
	"time"
	// "fmt"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"

	// "github.com/dgrijalva/jwt-go"
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
	client := &http.Client{
		Timeout: time.Second * 10,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	resp, err := client.Do(req)
	require.NoError(s.T(), err)
	assert.Equal(s.T(), http.StatusOK, resp.StatusCode)
}

func (s *registrationServiceTestSuite) TestEndpoints() {
	s.Run("verify_healthcheck", func() {
		// Call health endpoint.
		req, err := http.NewRequest("GET", s.route+"/api/v1/health", nil)
		require.NoError(s.T(), err)
		client := &http.Client{
			Timeout: time.Second * 10,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}

		resp, err := client.Do(req)
		require.NoError(s.T(), err)
		assert.Equal(s.T(), http.StatusOK, resp.StatusCode)

		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), body)

		mp := make(map[string]interface{})
		err = json.Unmarshal([]byte(body), &mp)
		require.Nil(s.T(), err)

		// Verify that fields exist in response.
		alive := mp["alive"]
		require.True(s.T(), alive.(bool))

		environment := mp["environment"]
		require.Equal(s.T(), "e2e-tests", environment.(string))

		revision := mp["revision"]
		require.NotNil(s.T(), revision)

		buildTime := mp["buildTime"]
		require.NotNil(s.T(), buildTime)

		startTime := mp["startTime"]
		require.NotNil(s.T(), startTime)
	})

	s.Run("verify_authconfig", func() {
		// Call authconfig endpoint.
		req, err := http.NewRequest("GET", s.route+"/api/v1/authconfig", nil)
		require.NoError(s.T(), err)
		client := &http.Client{
			Timeout: time.Second * 10,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}

		resp, err := client.Do(req)
		require.NoError(s.T(), err)
		assert.Equal(s.T(), http.StatusOK, resp.StatusCode)

		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		require.Nil(s.T(), err)
		require.NotNil(s.T(), body)

		mp := make(map[string]interface{})
		err = json.Unmarshal([]byte(body), &mp)
		require.Nil(s.T(), err)

		// Verify that fields exist in response.
		alive := mp["auth-client-library-url"]
		require.Equal(s.T(), alive.(string), "https://sso.prod-preview.openshift.io/auth/js/keycloak.js")

		testingMode := mp["auth-client-config"].(string)
		mp1 := make(map[string]interface{})
		err = json.Unmarshal([]byte(testingMode), &mp1)
		require.Nil(s.T(), err)

		realm := mp1["realm"]
		require.Equal(s.T(), realm.(string), "toolchain-public")

		authServerURL := mp1["auth-server-url"]
		require.Equal(s.T(), authServerURL.(string), "https://sso.prod-preview.openshift.io/auth")

		sslRequired := mp1["ssl-required"]
		require.Equal(s.T(), sslRequired.(string), "none")

		resource := mp1["resource"]
		require.Equal(s.T(), resource.(string), "crt")

		publicClient := mp1["public-client"]
		require.True(s.T(), publicClient.(bool))
	})

	s.Run("verify_signup_error_no_token", func() {
		// Call signup endpoint without a token.
		requestBody, err := json.Marshal(map[string]string{})
		require.Nil(s.T(), err)
		req, err := http.NewRequest("POST", s.route+"/api/v1/signup", bytes.NewBuffer(requestBody))
		require.NoError(s.T(), err)
		req.Header.Set("content-type", "application/json")
		client := &http.Client{
			Timeout: time.Second * 10,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}

		resp, err := client.Do(req)
		require.NoError(s.T(), err)

		// Retrieve unauthorized http status code.
		assert.Equal(s.T(), http.StatusUnauthorized, resp.StatusCode)

		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		require.Nil(s.T(), err)
		require.NotNil(s.T(), body)

		mp := make(map[string]interface{})
		err = json.Unmarshal([]byte(body), &mp)
		require.Nil(s.T(), err)

		// Check token error.
		tokenErr := mp["error"].(string)
		require.Equal(s.T(), "no token found", tokenErr)
	})

	s.Run("verify_signup_error_unknown_auth_header", func() {
		// Call signup endpoint with invalid authroization header.
		req, err := http.NewRequest("POST", s.route+"/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("Authorization", "1223123123")
		req.Header.Set("content-type", "application/json")
		client := &http.Client{
			Timeout: time.Second * 10,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}

		resp, err := client.Do(req)
		require.NoError(s.T(), err)

		// Retrieve unauthorized http status code.
		assert.Equal(s.T(), http.StatusUnauthorized, resp.StatusCode)

		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		require.Nil(s.T(), err)
		require.NotNil(s.T(), body)

		mp := make(map[string]interface{})
		err = json.Unmarshal([]byte(body), &mp)
		require.Nil(s.T(), err)

		// Check token error.
		tokenErr := mp["error"].(string)
		require.Equal(s.T(), "found unknown authorization header:1223123123", tokenErr)
	})

	s.Run("verify_signup_error_invalid_token", func() {
		// Call signup endpoint with an invalid token.
		req, err := http.NewRequest("POST", s.route+"/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("Authorization", "Bearer 1223123123")
		req.Header.Set("content-type", "application/json")
		client := &http.Client{
			Timeout: time.Second * 10,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}

		resp, err := client.Do(req)
		require.NoError(s.T(), err)

		// Retrieve unauthorized http status code.
		assert.Equal(s.T(), http.StatusUnauthorized, resp.StatusCode)

		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		require.Nil(s.T(), err)
		require.NotNil(s.T(), body)

		mp := make(map[string]interface{})
		err = json.Unmarshal([]byte(body), &mp)
		require.Nil(s.T(), err)

		// Check token error.
		tokenErr := mp["error"].(string)
		require.Equal(s.T(), "token contains an invalid number of segments", tokenErr)
	})

	s.Run("verify_signup_valid_token", func() {
		// Get valid generated token for e2e tests.
		identity := authsupport.NewIdentity()
		emailClaim0 := authsupport.WithEmailClaim(uuid.NewV4().String() + "@email.tld")
		token, err := authsupport.GenerateSignedE2ETestToken(*identity, emailClaim0)

		// Call signup endpoint with an valid token.
		req, err := http.NewRequest("POST", s.route+"/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("content-type", "application/json")
		client := &http.Client{
			Timeout: time.Second * 10,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}

		resp, err := client.Do(req)
		require.NoError(s.T(), err)

		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		require.NoError(s.T(), err)
		require.NotNil(s.T(), body)
		assert.Equal(s.T(), http.StatusAccepted, resp.StatusCode)
	})
}
