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
	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
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

func TestRegistrationServiceMetricsEndpoint(t *testing.T) {
	// given
	await := WaitForDeployments(t)
	t.Parallel()

	t.Run("not available from default route", func(t *testing.T) { // make sure that the `/metrics`` endpoint is NOT reachable with the default route
		// given
		route := await.Host().RegistrationServiceURL
		req, err := http.NewRequest("GET", route+"/metrics", nil)
		require.NoError(t, err)
		// when
		resp, err := httpClient.Do(req) // nolint:bodyclose // see `defer Close(t, resp)`
		// then
		require.NoError(t, err)
		defer Close(t, resp)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})

	t.Run("available from a custom route", func(t *testing.T) { // create a route for to expose the `registration-service-metrics` svc
		// given
		route := &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: await.Host().Namespace,
				Name:      "registration-service-metrics",
			},
			Spec: routev1.RouteSpec{
				To: routev1.RouteTargetReference{
					Kind: "Service",
					Name: "registration-service-metrics",
				},
				Port: &routev1.RoutePort{
					TargetPort: intstr.FromString("regsvc-metrics"),
				},
			},
		}
		err := await.Host().CreateWithCleanup(t, route)
		require.NoError(t, err)
		_, err = await.Host().WaitForRouteToBeAvailable(t, route.Namespace, route.Name, "/metrics")
		require.NoError(t, err, "route not available", route)

		req, err := http.NewRequest("GET", "http://"+route.Spec.Host+"/metrics", nil)
		require.NoError(t, err)
		// when
		resp, err := httpClient.Do(req) // nolint:bodyclose // see `defer Close(t, resp)`
		// then
		require.NoError(t, err)
		defer Close(t, resp)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		samples, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Contains(t, string(samples), "sandbox_promhttp_client_api_requests_total")
		assert.Contains(t, string(samples), "sandbox_promhttp_request_duration_seconds_bucket")
		assert.Contains(t, string(samples), "sandbox_promhttp_request_duration_seconds_sum")
		assert.Contains(t, string(samples), "sandbox_promhttp_request_duration_seconds_count")
	})
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

	assertNotSecuredGetResponseEquals := func(t *testing.T, endPointPath, expectedResponseValue string) {
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
		assertNotSecuredGetResponseEquals(t, "segment-write-key", "test devspaces segment write key")
	})

	t.Run("get sandbox segment write key 200 OK", func(t *testing.T) {
		// Call sandbox segment write key endpoint.
		assertNotSecuredGetResponseEquals(t, "analytics/segment-write-key", "test sandbox segment write key")
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
		mp := NewHTTPRequest(t).
			InvokeEndpoint("POST", route+"/api/v1/signup", "1223123123", "", http.StatusUnauthorized).
			UnmarshalMap()

		// Check token error.
		tokenErr := mp["error"]
		require.Equal(t, "token is malformed: token contains an invalid number of segments", tokenErr.(string))
	})
	t.Run("post signup exp token 401 Unauthorized", func(t *testing.T) {
		emailAddress := uuid.Must(uuid.NewV4()).String() + "@acme.com"
		// Not identical to the token used in POST signup - should return resource not found.
		_, token1, err := authsupport.NewToken(
			authsupport.WithEmail(emailAddress),
			authsupport.WithExp(time.Now().Add(-60*time.Second)))
		require.NoError(t, err)
		mp := NewHTTPRequest(t).
			InvokeEndpoint("POST", route+"/api/v1/signup", token1, "", http.StatusUnauthorized).
			UnmarshalMap()

		// Check token error.
		tokenErr := mp["error"]
		require.Contains(t, tokenErr.(string), "token has invalid claims: token is expired")
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
		mp := NewHTTPRequest(t).
			InvokeEndpoint("GET", route+"/api/v1/signup", "1223123123", "", http.StatusUnauthorized).
			UnmarshalMap()

		// Check token error.
		tokenErr := mp["error"]
		require.Equal(t, "token is malformed: token contains an invalid number of segments", tokenErr.(string))
	})
	t.Run("get signup exp token 401 Unauthorized", func(t *testing.T) {
		emailAddress := uuid.Must(uuid.NewV4()).String() + "@acme.com"
		// Not identical to the token used in POST signup - should return resource not found.
		_, token1, err := authsupport.NewToken(
			authsupport.WithEmail(emailAddress),
			authsupport.WithExp(time.Now().Add(-60*time.Second)))
		require.NoError(t, err)
		mp := NewHTTPRequest(t).
			InvokeEndpoint("GET", route+"/api/v1/signup", token1, "", http.StatusUnauthorized).
			UnmarshalMap()

		// Check token error.
		tokenErr := mp["error"]
		require.Contains(t, tokenErr.(string), "token has invalid claims: token is expired")
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
		response := NewHTTPRequest(t).
			InvokeEndpoint("POST", route+"/api/v1/signup", token, "", http.StatusForbidden).
			UnmarshalMap()
		require.Equal(t, "forbidden: failed to create usersignup for test-crtadmin", response["message"])
		require.Equal(t, "error creating UserSignup resource", response["details"])
		require.InDelta(t, float64(403), response["code"], 0.01)

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
		response := NewHTTPRequest(t).
			InvokeEndpoint("POST", route+"/api/v1/signup", token, "", http.StatusForbidden).
			UnmarshalMap()
		require.Equal(t, "forbidden: failed to create usersignup for longer-username-crtadmin", response["message"])
		require.Equal(t, "error creating UserSignup resource", response["details"])
		require.InDelta(t, float64(403), response["code"], 0.01)

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
		NewHTTPRequest(t).InvokeEndpoint("POST", route+"/api/v1/signup", token, "", http.StatusAccepted)

		// Wait for the UserSignup to be created
		userSignup, err := hostAwait.WaitForUserSignup(t, userSignupName,
			wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.PendingApproval())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValuePending))
		require.NoError(t, err)
		cleanup.AddCleanTasks(t, hostAwait.Client, userSignup)
		emailAnnotation := userSignup.Spec.IdentityClaims.Email
		assert.Equal(t, email, emailAnnotation)

		// Call get signup endpoint with a valid token and make sure it's pending approval
		assertGetSignupStatusPendingApproval(t, await, identity.Username, token)

		// Attempt to create same usersignup by calling post signup with same token should return an error
		mp := NewHTTPRequest(t).
			InvokeEndpoint("POST", route+"/api/v1/signup", token, "", http.StatusConflict).
			UnmarshalMap()
		assert.Equal(t, fmt.Sprintf("Operation cannot be fulfilled on  \"\": UserSignup [username: %s]. Unable to create UserSignup because there is already an active UserSignup with such a username",
			identity.Username), mp["message"])
		assert.Equal(t, "error creating UserSignup resource", mp["details"])

		userSignup, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
			Update(userSignup.Name, hostAwait.Namespace,
				func(instance *toolchainv1alpha1.UserSignup) {
					// Approve usersignup.
					states.SetApprovedManually(instance, true)
					instance.Spec.TargetCluster = memberAwait.ClusterName
				})
		require.NoError(t, err)

		// Wait for the resources to be provisioned
		VerifyResourcesProvisionedForSignup(t, await, userSignup)

		// Call signup endpoint with same valid token to check if status changed to Provisioned now
		now := time.Now()
		NewGetSignupClient(t, await, identity.Username, token).Invoke(signupIsProvisioned,
			signupHasExpectedDates(now, now.Add(time.Hour*24*30)))

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

		t.Logf("Signed up new user %+v", userSignup)

		// Deactivate the usersignup
		userSignup, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
			Update(userSignup.Name, hostAwait.Namespace,
				func(us *toolchainv1alpha1.UserSignup) {
					states.SetDeactivated(us, true)
				})
		require.NoError(t, err)
		_, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.DeactivatedWithoutPreDeactivation())...),
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
		require.NoError(t, err)
		require.NoError(t, hostAwait.WaitUntilSpaceAndSpaceBindingsDeleted(t, userSignup.Status.CompliantUsername))

		// Now check that the reg-service treats the deactivated usersignup as nonexistent and returns 404
		assertGetSignupReturnsNotFound(t, await, token)

		// Re-activate the usersignup by calling the signup endpoint with the same token/user again
		signupUser(token, emailAddress, identity.Username, identity)
	})

	t.Run("test get signup returns expected response", func(t *testing.T) {
		// Get valid generated token for e2e tests.
		emailAddress := uuid.Must(uuid.NewV4()).String() + "@acme.com"
		identity, token, err := authsupport.NewToken(authsupport.WithEmail(emailAddress), commonauth.WithUserIDClaim("123"), commonauth.WithAccountIDClaim("456"), commonauth.WithAccountNumberClaim("789"))
		require.NoError(t, err)

		// Signup a new user
		userSignup := signupUser(token, emailAddress, identity.Username, identity)

		t.Logf("Signed up new user %+v", userSignup)

		// call GET /signup and verify expected response body fields are there
		now := time.Now()
		NewGetSignupClient(t, await, identity.Username, token).Invoke(signupIsProvisioned,
			signupHasExpectedDates(now, now.Add(time.Hour*24*30)),
			signupHasExpectedClaims(map[string]string{
				"name":          userSignup.Name,
				"username":      userSignup.Spec.IdentityClaims.PreferredUsername,
				"givenName":     userSignup.Spec.IdentityClaims.GivenName,
				"familyName":    userSignup.Spec.IdentityClaims.FamilyName,
				"userID":        "123",
				"accountID":     "456",
				"accountNumber": "789",
			}))
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
	NewHTTPRequest(t).InvokeEndpoint("POST", route+"/api/v1/signup", token0, "", http.StatusAccepted)

	// Wait for the UserSignup to be created
	userSignup, err := hostAwait.WaitForUserSignup(t, "arnold",
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.PendingApproval())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValuePending))
	require.NoError(t, err)
	cleanup.AddCleanTasks(t, hostAwait.Client, userSignup)
	email := userSignup.Spec.IdentityClaims.Email
	assert.Equal(t, emailAddress, email)

	// Call get signup endpoint with a valid token, however we will now override the claims to introduce the original
	// sub claim and set username as a separate claim, then we will make sure the UserSignup is returned correctly
	_, token0, err = authsupport.NewToken(
		authsupport.WithEmail(emailAddress),
		authsupport.WithPreferredUsername("arnold"))
	require.NoError(t, err)
	mp, mpStatus := ParseSignupResponse(t, NewHTTPRequest(t).InvokeEndpoint("GET", route+"/api/v1/signup", token0, "", http.StatusOK).UnmarshalMap())
	assert.Empty(t, mp["compliantUsername"])
	assert.Equal(t, "arnold", mp["username"], "got response %+v", mp)
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
	NewHTTPRequest(t).
		InvokeEndpoint("POST", route+"/api/v1/signup", token0, "", http.StatusAccepted).
		UnmarshalMap()

	// Wait for the UserSignup to be created
	userSignup, err := hostAwait.WaitForUserSignup(t, identity0.Username,
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(t, err)
	cleanup.AddCleanTasks(t, hostAwait.Client, userSignup)
	email := userSignup.Spec.IdentityClaims.Email
	assert.Equal(t, emailAddress, email)

	// Call get signup endpoint with a valid token and make sure verificationRequired is true
	mp, mpStatus := ParseSignupResponse(t, NewHTTPRequest(t).InvokeEndpoint("GET", route+"/api/v1/signup", token0, "", http.StatusOK).UnmarshalMap())
	assert.Empty(t, mp["compliantUsername"])
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
	require.True(t, apierrors.IsNotFound(err))

	// Initiate the verification process
	NewHTTPRequest(t).
		InvokeEndpoint("PUT", route+"/api/v1/signup/verification", token0, `{ "country_code":"+61", "phone_number":"408999999" }`, http.StatusNoContent)

	// Retrieve the updated UserSignup
	userSignup, err = hostAwait.WaitForUserSignup(t, identity0.Username)
	require.NoError(t, err)

	// Confirm there is a verification code annotation value, and store it in a variable
	verificationCode := userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey]
	require.NotEmpty(t, verificationCode)

	// Confirm the expiry time has been set
	require.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserVerificationExpiryAnnotationKey])

	// Attempt to verify with an incorrect verification code
	NewHTTPRequest(t).InvokeEndpoint("GET", route+"/api/v1/signup/verification/invalid", token0, "", http.StatusForbidden)

	// Retrieve the updated UserSignup
	userSignup, err = hostAwait.WaitForUserSignup(t, identity0.Username)
	require.NoError(t, err)

	// Check attempts has been incremented
	require.NotEmpty(t, userSignup.Annotations[toolchainv1alpha1.UserVerificationAttemptsAnnotationKey])

	// Confirm the verification code has not changed
	require.Equal(t, verificationCode, userSignup.Annotations[toolchainv1alpha1.UserSignupVerificationCodeAnnotationKey])

	// Verify with the correct code
	NewHTTPRequest(t).InvokeEndpoint("GET", route+fmt.Sprintf("/api/v1/signup/verification/%s",
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
	mp, mpStatus = ParseSignupResponse(t, NewHTTPRequest(t).InvokeEndpoint("GET", route+"/api/v1/signup", token0, "", http.StatusOK).UnmarshalMap())
	assert.Empty(t, mp["compliantUsername"])
	assert.Empty(t, mp["defaultUserNamespace"])
	assert.Empty(t, mp["rhodsMemberURL"])
	assert.Empty(t, mp["cheDashboardURL"])
	assert.Equal(t, identity0.Username, mp["username"])
	require.IsType(t, false, mpStatus["ready"])
	assert.False(t, mpStatus["ready"].(bool))
	assert.Equal(t, "PendingApproval", mpStatus["reason"])
	require.False(t, mpStatus["verificationRequired"].(bool))

	userSignup, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
		Update(userSignup.Name, hostAwait.Namespace,
			func(instance *toolchainv1alpha1.UserSignup) {
				// Now approve the usersignup.
				states.SetApprovedManually(instance, true)
			})
	require.NoError(t, err)
	transformedUsername := commonsignup.TransformUsername(userSignup.Spec.IdentityClaims.PreferredUsername, []string{"openshift", "kube", "default", "redhat", "sandbox"}, []string{"admin"})
	// Confirm the MasterUserRecord is provisioned
	_, err = hostAwait.WaitForMasterUserRecord(t, transformedUsername, wait.UntilMasterUserRecordHasCondition(wait.Provisioned()))
	require.NoError(t, err)

	// Retrieve the UserSignup from the GET endpoint
	_, mpStatus = ParseSignupResponse(t, NewHTTPRequest(t).InvokeEndpoint("GET", route+"/api/v1/signup", token0, "", http.StatusOK).UnmarshalMap())

	// Confirm that VerificationRequired is no longer true
	require.False(t, mpStatus["verificationRequired"].(bool))

	// Create another token and identity to sign up with
	otherEmailValue := uuid.Must(uuid.NewV4()).String() + "@other.domain"
	otherIdentity, otherToken, err := authsupport.NewToken(authsupport.WithEmail(otherEmailValue))
	require.NoError(t, err)

	// Call the signup endpoint
	NewHTTPRequest(t).InvokeEndpoint("POST", route+"/api/v1/signup", otherToken, "", http.StatusAccepted)

	// Wait for the UserSignup to be created
	otherUserSignup, err := hostAwait.WaitForUserSignup(t, otherIdentity.Username,
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(t, err)
	cleanup.AddCleanTasks(t, hostAwait.Client, otherUserSignup)
	otherEmailAnnotation := otherUserSignup.Spec.IdentityClaims.Email
	assert.Equal(t, otherEmailValue, otherEmailAnnotation)

	// Initiate the verification process using the same phone number as previously
	responseMap := NewHTTPRequest(t).
		InvokeEndpoint("PUT", route+"/api/v1/signup/verification", otherToken,
			`{ "country_code":"+61", "phone_number":"408999999" }`, http.StatusForbidden).UnmarshalMap()

	require.NotEmpty(t, responseMap)
	require.InDelta(t, float64(http.StatusForbidden), responseMap["code"], 0.01, "code not found in response body map %s", responseMap)

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

	userSignup, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
		Update(userSignup.Name, hostAwait.Namespace,
			func(instance *toolchainv1alpha1.UserSignup) {
				// Now mark the original UserSignup as deactivated
				states.SetDeactivated(instance, true)
			})
	require.NoError(t, err)

	// Ensure the UserSignup is deactivated
	_, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ManuallyDeactivated())...))
	require.NoError(t, err)

	// Now attempt the verification again
	NewHTTPRequest(t).
		InvokeEndpoint("PUT", route+"/api/v1/signup/verification", otherToken, `{ "country_code":"+61", "phone_number":"408999999" }`, http.StatusNoContent)

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
	member2Await := await.Member2()
	route := hostAwait.RegistrationServiceURL

	verifySuccessful := func(t *testing.T, targetCluster string, existingUserSignup, deactivateSignup bool) {
		// given
		event := testsocialevent.NewSocialEvent(hostAwait.Namespace, commonsocialevent.NewName(),
			testsocialevent.WithUserTier("deactivate80"),
			testsocialevent.WithSpaceTier("base1ns6didler"),
			testsocialevent.WithTargetCluster(targetCluster))
		err := hostAwait.CreateWithCleanup(t, event)
		require.NoError(t, err)
		userSignup := &toolchainv1alpha1.UserSignup{}
		token, userName := "", ""
		if existingUserSignup {
			// create a user signup before using the activation code
			userSignup, token = signup(t, hostAwait)
			cleanup.AddCleanTasks(t, hostAwait.Client, userSignup)
			userName = userSignup.Name

			if deactivateSignup {
				// deactivate the UserSignup to test the deactivation path
				_, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
					Update(userName, hostAwait.Namespace,
						func(us *toolchainv1alpha1.UserSignup) {
							states.SetDeactivated(us, true)
						})
				require.NoError(t, err)
				t.Logf("user signup '%s' deactivated", userName)
			}
		} else {
			// create only the identity and token for calling the activation-code endpoint.
			// the signup should be created by the activation-code endpoint.
			identity, _, tokenValue := userToken(t)
			token = tokenValue
			userName = identity.Username
		}

		// when call verification endpoint with a valid activation code
		NewHTTPRequest(t).
			InvokeEndpoint("POST", route+"/api/v1/signup/verification/activation-code", token, fmt.Sprintf(`{"code":"%s"}`, event.Name), http.StatusOK)

		// then
		userSignup, err = hostAwait.WaitForUserSignup(t, userName,
			wait.UntilUserSignupHasLabel(toolchainv1alpha1.SocialEventUserSignupLabelKey, event.Name),
			wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...))
		cleanup.AddCleanTasks(t, hostAwait.Client, userSignup)
		require.NoError(t, err)
		// explicitly approve the usersignup (see above, config for parallel test has automatic approval disabled)
		_, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
			Update(userName, hostAwait.Namespace,
				func(us *toolchainv1alpha1.UserSignup) {
					states.SetApprovedManually(us, true)
				})
		require.NoError(t, err)
		t.Logf("user signup '%s' approved", userName)

		// check that the MUR and Space are configured as expected
		// Wait for the UserSignup to have the desired state
		userSignup, err = hostAwait.WaitForUserSignup(t, userName,
			wait.UntilUserSignupHasCompliantUsername(),
			wait.UntilUserSignupHasTargetCluster(targetCluster))
		require.NoError(t, err)
		mur, err := hostAwait.WaitForMasterUserRecord(t, userSignup.Status.CompliantUsername,
			wait.UntilMasterUserRecordHasTierName(event.Spec.UserTier),
			wait.UntilMasterUserRecordHasCondition(wait.Provisioned()))
		require.NoError(t, err)
		assert.Equal(t, event.Spec.UserTier, mur.Spec.TierName)
		spaceCriterion := []wait.SpaceWaitCriterion{
			wait.UntilSpaceHasTier(event.Spec.SpaceTier),
			wait.UntilSpaceHasConditions(wait.Provisioned()),
		}
		if targetCluster != "" {
			spaceCriterion = append(spaceCriterion, wait.UntilSpaceHasStatusTargetCluster(targetCluster))
		} else {
			spaceCriterion = append(spaceCriterion, wait.UntilSpaceHasAnyTargetClusterSet())
		}
		_, err = hostAwait.WaitForSpace(t, userSignup.Status.CompliantUsername, spaceCriterion...)
		require.NoError(t, err)

		// also check that the SocialEvent status was updated accordingly
		_, err = hostAwait.WaitForSocialEvent(t, event.Name, wait.UntilSocialEventHasActivationCount(1))
		require.NoError(t, err)
	}

	t.Run("verification successful with no target cluster", func(t *testing.T) {
		verifySuccessful(t, "", true, false)
	})

	t.Run("verification successful with target cluster", func(t *testing.T) {
		verifySuccessful(t, member2Await.ClusterName, true, false)
	})

	t.Run("UserSignup doesn't exist yet it should be created", func(t *testing.T) {
		verifySuccessful(t, member2Await.ClusterName, false, false)
	})

	t.Run("UserSignup is deactivated it should be reactivated", func(t *testing.T) {
		verifySuccessful(t, member2Await.ClusterName, true, true)
	})

	t.Run("verification failed", func(t *testing.T) {
		t.Run("unknown code", func(t *testing.T) {
			// given
			userSignup, token := signup(t, hostAwait)

			// when call verification endpoint with a valid activation code
			NewHTTPRequest(t).
				InvokeEndpoint("POST", route+"/api/v1/signup/verification/activation-code", token, fmt.Sprintf(`{"code":"%s"}`, "unknown"), http.StatusForbidden)

			// then
			// ensure the UserSignup is not approved yet
			userSignup, err := hostAwait.WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...))
			require.NoError(t, err)
			assert.Equal(t, "1", userSignup.Annotations[toolchainv1alpha1.UserVerificationAttemptsAnnotationKey])
		})

		t.Run("over capacity", func(t *testing.T) {
			// given
			event := testsocialevent.NewSocialEvent(hostAwait.Namespace, commonsocialevent.NewName(),
				testsocialevent.WithUserTier("deactivate80"),
				testsocialevent.WithSpaceTier("base1ns6didler"),
				testsocialevent.WithTargetCluster(member2Await.ClusterName))
			err := hostAwait.CreateWithCleanup(t, event)
			require.NoError(t, err)
			event, err = hostAwait.WaitForSocialEvent(t, event.Name, wait.UntilSocialEventHasConditions(toolchainv1alpha1.Condition{
				Type:   toolchainv1alpha1.ConditionReady,
				Status: corev1.ConditionTrue,
			})) // need to reload event
			require.NoError(t, err)
			event, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.SocialEvent{}).
				UpdateStatus(event.Name, hostAwait.Namespace,
					func(ev *toolchainv1alpha1.SocialEvent) {
						ev.Status.ActivationCount = event.Spec.MaxAttendees // activation count identical to `MaxAttendees`
					})
			require.NoError(t, err)

			userSignup, token := signup(t, hostAwait)

			// when call verification endpoint with a valid activation code
			NewHTTPRequest(t).
				InvokeEndpoint("POST", route+"/api/v1/signup/verification/activation-code", token, fmt.Sprintf(`{"code":"%s"}`, event.Name), http.StatusForbidden)

			// then
			// ensure the UserSignup is not approved yet
			userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...),
				wait.UntilUserSignupHasTargetCluster("")) // target cluster from the event is ignored when verification failed
			require.NoError(t, err)
			assert.Equal(t, "1", userSignup.Annotations[toolchainv1alpha1.UserVerificationAttemptsAnnotationKey])
		})

		t.Run("not opened yet", func(t *testing.T) {
			// given
			event := testsocialevent.NewSocialEvent(hostAwait.Namespace, commonsocialevent.NewName(),
				testsocialevent.WithStartTime(time.Now().Add(time.Hour)), // not open yet
				testsocialevent.WithTargetCluster(member2Await.ClusterName))
			err := hostAwait.CreateWithCleanup(t, event)
			require.NoError(t, err)
			userSignup, token := signup(t, hostAwait)

			// when call verification endpoint with a valid activation code
			NewHTTPRequest(t).
				InvokeEndpoint("POST", route+"/api/v1/signup/verification/activation-code", token, fmt.Sprintf(`{"code":"%s"}`, event.Name), http.StatusForbidden)

			// then
			// ensure the UserSignup is not approved yet
			userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...),
				wait.UntilUserSignupHasTargetCluster("")) // target cluster from the event is ignored when verification failed
			require.NoError(t, err)
			assert.Equal(t, "1", userSignup.Annotations[toolchainv1alpha1.UserVerificationAttemptsAnnotationKey])
		})

		t.Run("already closed", func(t *testing.T) {
			// given
			event := testsocialevent.NewSocialEvent(hostAwait.Namespace,
				commonsocialevent.NewName(),
				testsocialevent.WithEndTime(time.Now().Add(-time.Hour)), // already closd
				testsocialevent.WithTargetCluster(member2Await.ClusterName))
			err := hostAwait.CreateWithCleanup(t, event)
			require.NoError(t, err)
			userSignup, token := signup(t, hostAwait)

			// when call verification endpoint with a valid activation code
			NewHTTPRequest(t).
				InvokeEndpoint("POST", route+"/api/v1/signup/verification/activation-code", token, fmt.Sprintf(`{"code":"%s"}`, event.Name), http.StatusForbidden)

			// then
			// ensure the UserSignup is approved
			userSignup, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
				wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...),
				wait.UntilUserSignupHasTargetCluster("")) // target cluster from the event is ignored when verification failed
			require.NoError(t, err)
			assert.Equal(t, "1", userSignup.Annotations[toolchainv1alpha1.UserVerificationAttemptsAnnotationKey])
		})
	})
}

func TestUsernames(t *testing.T) {
	// given
	t.Parallel()
	awaitilities := WaitForDeployments(t)
	route := awaitilities.Host().RegistrationServiceURL
	// Create a token that will be used to invoke the /usernames api
	identity := commonauth.NewIdentity()
	emailValue := identity.Username + "@some.domain"
	emailClaim := commonauth.WithEmailClaim(emailValue)
	token, err := commonauth.GenerateSignedE2ETestToken(*identity, emailClaim)
	require.NoError(t, err)

	t.Run("get usernames 200 response", func(t *testing.T) {
		// given
		// we have a user in the system
		user := NewSignupRequest(awaitilities).
			Username("testgetusernames").
			Email("testgetusernames@redhat.com").
			ManuallyApprove().
			TargetCluster(awaitilities.Member1()).
			EnsureMUR().
			RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
			NoSpace().
			Execute(t)
		// when
		// we call the get usernames endpoint to get the user
		response := NewHTTPRequest(t).
			InvokeEndpoint("GET", route+"/api/v1/usernames/"+user.MUR.GetName(), token, "", http.StatusOK).
			UnmarshalSlice()

		// then
		assert.Len(t, response, 1)                                   // only one user should be returned
		assert.Equal(t, "testgetusernames", response[0]["username"]) // the username should match
	})

	t.Run("get usernames 404 response", func(t *testing.T) {
		// given
		// we search for some invalid username strings
		tests := map[string]struct {
			searchQuery string
		}{
			"username not found": {
				searchQuery: "noise", // user doesn't exist
			},
			"username string is empty": {
				searchQuery: "", // no username provided
			},
		}

		for testName, tc := range tests {
			t.Run(testName, func(t *testing.T) {
				// when/then
				NewHTTPRequest(t).
					InvokeEndpoint("GET", route+"/api/v1/usernames/"+tc.searchQuery, token, "", http.StatusNotFound)
			})
		}
	})
}

func signup(t *testing.T, hostAwait *wait.HostAwaitility) (*toolchainv1alpha1.UserSignup, string) {
	route := hostAwait.RegistrationServiceURL

	// Create a token and identity to sign up with
	identity, emailValue, token := userToken(t)

	// Call the signup endpoint
	NewHTTPRequest(t).InvokeEndpoint("POST", route+"/api/v1/signup", token, "", http.StatusAccepted)

	// Wait for the UserSignup to be created
	userSignup, err := hostAwait.WaitForUserSignup(t, identity.Username,
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.VerificationRequired())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueNotReady))
	require.NoError(t, err)
	cleanup.AddCleanTasks(t, hostAwait.Client, userSignup)
	email := userSignup.Spec.IdentityClaims.Email
	assert.Equal(t, emailValue, email)
	return userSignup, token
}

func userToken(t *testing.T) (*commonauth.Identity, string, string) {
	identity := commonauth.NewIdentity()
	emailValue := identity.Username + "@some.domain"
	emailClaim := commonauth.WithEmailClaim(emailValue)
	token, err := commonauth.GenerateSignedE2ETestToken(*identity, emailClaim)
	require.NoError(t, err)
	return identity, emailValue, token
}

func signupHasExpectedDates(startDate, endDate time.Time) func(c *GetSignupClient) {
	return func(c *GetSignupClient) {
		responseStartDate, err := time.Parse(time.RFC3339, c.responseBody["startDate"].(string))
		require.NoError(c.t, err)
		require.WithinDuration(c.t, startDate, responseStartDate, time.Hour,
			"startDate in response [%s] not in expected range [%s]", responseStartDate, startDate.Format(time.RFC3339))

		responseEndDate, err := time.Parse(time.RFC3339, c.responseBody["endDate"].(string))
		require.NoError(c.t, err)
		require.WithinDuration(c.t, endDate, responseEndDate, time.Hour,
			"endDate in response [%s] not in expected range [%s]", responseEndDate, endDate.Format(time.RFC3339))
	}
}

func signupHasExpectedClaims(claims map[string]string) func(c *GetSignupClient) {
	return func(c *GetSignupClient) {
		for expectedClaim, expectedClaimValue := range claims {
			actualClaimValue, claimFound := c.responseBody[expectedClaim]
			require.True(c.t, claimFound, "unable to find expected claim [%s]. Claims found %v", expectedClaim, claims)
			require.Equal(c.t, expectedClaimValue, actualClaimValue, "expected claim value [%s] doesn't match actual claim value [%s]", expectedClaimValue, actualClaimValue)
		}
	}
}

func signupIsProvisioned(client *GetSignupClient) {
	hostAwait := client.await.Host()
	memberAwait := client.await.Member1()
	memberCluster, found, err := hostAwait.GetToolchainCluster(client.t, memberAwait.Namespace, toolchainv1alpha1.ConditionReady)
	transformedUsername := commonsignup.TransformUsername(client.username, []string{"openshift", "kube", "default", "redhat", "sandbox"}, []string{"admin"})
	require.NoError(client.t, err)
	require.True(client.t, found)
	assert.Equal(client.t, memberCluster.Status.APIEndpoint, client.responseBody["apiEndpoint"])
	assert.Equal(client.t, hostAwait.APIProxyURL, client.responseBody["proxyURL"])
	assert.Equal(client.t, fmt.Sprintf("%s-dev", transformedUsername), client.responseBody["defaultUserNamespace"])
	assertResponseURL(client.t, memberAwait, "rhods", client.responseBody)
	assertResponseURL(client.t, memberAwait, "devspaces", client.responseBody)
}

type GetSignupClient struct {
	t            *testing.T
	await        wait.Awaitilities
	username     string
	bearerToken  string
	responseBody map[string]interface{}
}

func NewGetSignupClient(t *testing.T, await wait.Awaitilities, username, bearerToken string) *GetSignupClient {
	return &GetSignupClient{
		t:           t,
		await:       await,
		username:    username,
		bearerToken: bearerToken,
	}
}

func (c *GetSignupClient) Invoke(assertions ...func(client *GetSignupClient)) {
	hostAwait := c.await.Host()
	memberAwait := c.await.Member1()
	c.responseBody = waitForUserSignupReadyInRegistrationService(c.t, hostAwait.RegistrationServiceURL, c.username, c.bearerToken)
	transformedUsername := commonsignup.TransformUsername(c.username, []string{"openshift", "kube", "default", "redhat", "sandbox"}, []string{"admin"})
	assert.Equal(c.t, transformedUsername, c.responseBody["compliantUsername"])
	assert.Equal(c.t, c.username, c.responseBody["username"])
	assert.Equal(c.t, memberAwait.GetConsoleURL(c.t), c.responseBody["consoleURL"])

	for _, assertion := range assertions {
		assertion(c)
	}
}

func assertGetSignupStatusPendingApproval(t *testing.T, await wait.Awaitilities, username, bearerToken string) {
	route := await.Host().RegistrationServiceURL
	mp, mpStatus := ParseSignupResponse(t, NewHTTPRequest(t).InvokeEndpoint("GET", route+"/api/v1/signup", bearerToken, "", http.StatusOK).UnmarshalMap())
	assert.Equal(t, username, mp["username"], "unexpected username in response", mp, mpStatus)
	assert.Empty(t, mp["defaultUserNamespace"])
	assert.Empty(t, mp["rhodsMemberURL"])
	assert.Empty(t, mp["cheDashboardURL"])
	require.IsType(t, false, mpStatus["ready"])
	assert.False(t, mpStatus["ready"].(bool))
	assert.Equal(t, "PendingApproval", mpStatus["reason"])
}

func assertGetSignupReturnsNotFound(t *testing.T, await wait.Awaitilities, bearerToken string) {
	route := await.Host().RegistrationServiceURL
	NewHTTPRequest(t).InvokeEndpoint("GET", route+"/api/v1/signup", bearerToken, "", http.StatusNotFound)
}

func assertResponseURL(t *testing.T, memberAwait *wait.MemberAwaitility, urlPrefix string, responseField map[string]interface{}) {
	require.Containsf(t, memberAwait.GetConsoleURL(t), ".apps", "expected to find .apps in the console URL %s", memberAwait.GetConsoleURL(t))
	index := strings.Index(memberAwait.GetConsoleURL(t), ".apps")
	appsURL := memberAwait.GetConsoleURL(t)[index:]

	urlMappings := map[string]struct {
		hostname    string
		responseKey string
	}{
		"devspaces": {"devspaces", "cheDashboardURL"},
		"rhods":     {"rhods-dashboard-redhat-ods-applications", "rhodsMemberURL"},
	}

	mapping := urlMappings[urlPrefix]
	expectedURL := fmt.Sprintf("https://%s%s", mapping.hostname, appsURL)
	assert.Equal(t, expectedURL, responseField[mapping.responseKey])
}

// waitForUserSignupReadyInRegistrationService waits and checks that the UserSignup is ready according to registration service /signup endpoint
func waitForUserSignupReadyInRegistrationService(t *testing.T, registrationServiceURL, name, bearerToken string) map[string]interface{} {
	t.Logf("waiting and verifying that UserSignup '%s' is ready according to registration service", name)
	var mp, mpStatus map[string]interface{}
	err := k8swait.PollUntilContextTimeout(context.TODO(), time.Second, time.Second*60, true, func(ctx context.Context) (done bool, err error) {
		mp, mpStatus = ParseSignupResponse(t, NewHTTPRequest(t).InvokeEndpoint("GET", registrationServiceURL+"/api/v1/signup", bearerToken, "", http.StatusOK).UnmarshalMap())
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
