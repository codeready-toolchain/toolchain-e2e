package e2e

import (
	//"os"
	"bytes"
	"crypto/tls"
	"net/http"
	"testing"
	"time"
	"encoding/json"
    "io/ioutil"
   // "fmt"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/wait"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"

   // "github.com/dgrijalva/jwt-go"
	uuid "github.com/satori/go.uuid"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
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
     req, err := http.NewRequest("GET", s.route + "/api/v1/health", nil)
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
     req, err := http.NewRequest("GET", s.route + "/api/v1/authconfig", nil)
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

     // confidentialPort := mp1["confidential-port"]
     // require.Equal(s.T(), int(confidentialPort.(float64)), 0)
 })

 s.Run("verify_signup_error_no_token", func() {
     requestBody, err := json.Marshal(map[string]string{})
     require.Nil(s.T(), err)
     req, err := http.NewRequest("POST", s.route + "/api/v1/signup",  bytes.NewBuffer(requestBody))
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
     assert.Equal(s.T(), http.StatusUnauthorized, resp.StatusCode)

     defer resp.Body.Close()

     body, err := ioutil.ReadAll(resp.Body)
     require.Nil(s.T(), err)
     require.NotNil(s.T(), body)

     mp := make(map[string]interface{})
     err = json.Unmarshal([]byte(body), &mp)
     require.Nil(s.T(), err)

     tokenErr := mp["error"].(string)
     require.Equal(s.T(), "no token found", tokenErr)
 })

 s.Run("verify_signup_error_unknown_auth_header", func() {
     req, err := http.NewRequest("POST", s.route + "/api/v1/signup", nil)
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
     assert.Equal(s.T(), http.StatusUnauthorized, resp.StatusCode)
        
     defer resp.Body.Close()

     body, err := ioutil.ReadAll(resp.Body)
     require.Nil(s.T(), err)
     require.NotNil(s.T(), body)

     mp := make(map[string]interface{})
     err = json.Unmarshal([]byte(body), &mp)
     require.Nil(s.T(), err)

     tokenErr := mp["error"].(string)
     require.Equal(s.T(), "found unknown authorization header:1223123123", tokenErr)
 })

    s.Run("verify_signup_error_invalid_token", func() {
     req, err := http.NewRequest("POST", s.route + "/api/v1/signup", nil)
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
     assert.Equal(s.T(), http.StatusUnauthorized, resp.StatusCode)

     defer resp.Body.Close()

     body, err := ioutil.ReadAll(resp.Body)
     require.Nil(s.T(), err)
     require.NotNil(s.T(), body)

     mp := make(map[string]interface{})
     err = json.Unmarshal([]byte(body), &mp)
     require.Nil(s.T(), err)

     tokenErr := mp["error"].(string)
     require.Equal(s.T(), "token contains an invalid number of segments", tokenErr)
    })

    s.Run("verify_signup_valid_token", func() {

		// tokenManager := authsupport.NewTokenManager()
		// kid0 := uuid.NewV4().String()
			
		// //1. Create Keypair. --- AddPrivateKey() with kid
		// key, err := tokenManager.AddPrivateKey(kid0)
		// require.NoError(s.T(), err)

		// 2/3. Create Token. GenerateSignedToken(). Sign Token with Private Key. -- use func SignToken()
		identity := authsupport.NewIdentity()
		emailClaim0 := authsupport.WithEmailClaim(uuid.NewV4().String() + "@email.tld")
        token, err := authsupport.GenerateSignedE2ETestToken(*identity, emailClaim0)
       // time.Sleep(2 * time.Second)
        // publicKeys := authsupport.GetE2ETestPublicKey()
		// require.Len(s.T(), publicKeys, 1)
		// publicKey := publicKeys[0]
        // parsedToken, err := jwt.Parse(token, func(token *jwt.Token) (interface{}, error) {
		// 	kid := token.Header["kid"]
		// 	require.NotNil(s.T(), kid)
		// 	kidStr := kid.(string)
		// 	assert.Equal(s.T(), publicKey.KeyID, kidStr)

		// 	return publicKey.Key, nil
        // })
        
        // require.Nil(s.T(), err)
        // fmt.Println("E2E-TESTS")
        // fmt.Println(parsedToken.Claims.(jwt.MapClaims)["iat"])
        // fmt.Println(parsedToken.Claims.(jwt.MapClaims)["nbf"])
        // fmt.Println(parsedToken.Claims.(jwt.MapClaims)["exp"])
        // fmt.Println(time.Now().Unix())
        //require.Equal(s.T(), parsedToken, "")
		// // // 4/5. Convert Public Key to JWK JSON Format and return
		// keyServer := authsupport.NewJWKServer(key, kid0)
		
		// keysEndpointURL := keyServer.URL
		// reg, err := configuration.New("")
		
		// // Set auth_client.public_keys_url  to that address.
		// os.Setenv("REGISTRATION_AUTH_CLIENT_PUBLIC_KEYS_URL", keysEndpointURL)
		// os.Setenv("REGISTRATION_TESTINGMODE", "false")

		// Send Token in Header to Service.
		req, err := http.NewRequest("POST", s.route + "/api/v1/signup", nil)
		require.NoError(s.T(), err)
		req.Header.Set("Authorization", "Bearer " + token)
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
		require.Nil(s.T(), err)
		require.NotNil(s.T(), body)

		// mp := make(map[string]interface{})
		// err = json.Unmarshal([]byte(body), &mp)
		// require.Nil(s.T(), err)

		// tokenErr := mp["error"].(string)
		// require.Equal(s.T(), "token contains an invalid number of segments", tokenErr)
	 
		assert.Equal(s.T(), http.StatusOK, resp.StatusCode)
    })
}