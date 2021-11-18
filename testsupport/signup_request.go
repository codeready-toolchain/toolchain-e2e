package testsupport

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
)

var httpClient = HTTPClient

// SignupRequest provides an API for creating a new UserSignup via the registration service REST endpoint. It operates
// with a set of sensible default values which can be overridden via its various functions.  Function chaining may
// be used to achieve an efficient "single-statement" UserSignup creation, for example:
//
// userSignupMember1, murMember1 := s.newUserRequest().
//			Username("sample-username").
//			Email("sample-user@redhat.com").
//			ManuallyApprove().
//			EnsureMUR().
//			RequireConditions(ConditionSet(Default(), ApprovedByAdmin())...).
//			Execute().Resources()
//
type SignupRequest interface {
	// Email specifies the email address to use for the new UserSignup
	Email(email string) SignupRequest

	// OriginalSub specifies the original sub value which will be used for migrating the user to a new IdP client
	OriginalSub(originalSub string) SignupRequest

	// EnsureMUR will ensure that a MasterUserRecord is created.  It is necessary to call this function in order for
	// the Resources() function to return a non-nil value for its second return parameter.
	EnsureMUR() SignupRequest

	// Execute executes the request against the Registration service REST endpoint.  This function may only be called
	// once, and must be called after all other functions EXCEPT for Resources()
	Execute() SignupRequest

	// ManuallyApprove if called will set the "approved" state to true after the UserSignup has been created
	ManuallyApprove() SignupRequest

	// RequireConditions specifies the condition values that the new UserSignup is required to have in order for
	// the signup to be considered successful
	RequireConditions(conditions ...toolchainv1alpha1.Condition) SignupRequest

	// RequireHTTPStatus may be used to override the expected HTTP response code received from the Registration Service.
	// If not specified, here, the default expected value is StatusAccepted
	RequireHTTPStatus(httpStatus int) SignupRequest

	// Resources may be called only after a call to Execute().  It returns two parameters; the first is the UserSignup
	// instance that was created, the second is the MasterUserRecord instance, HOWEVER the MUR will only be returned
	// here if EnsureMUR() was also called previously, otherwise a nil value will be returned
	Resources() (*toolchainv1alpha1.UserSignup, *toolchainv1alpha1.MasterUserRecord)

	// TargetCluster may be provided in order to specify the user's target cluster
	TargetCluster(targetCluster *wait.MemberAwaitility) SignupRequest

	// IdentityID specifies the ID value for the user's Identity.  This value if set will be used to set both the
	// "Subject" and "IdentityID" claims in the user's auth token.  If not set, a new UUID value will be used
	IdentityID(id uuid.UUID) SignupRequest

	// Username specifies the username of the user
	Username(username string) SignupRequest

	// VerificationRequired specifies that the "verification-required" state will be set for the new UserSignup, however
	// if ManuallyApprove() is also called then this will have no effect as user approval overrides the verification
	// required state.
	VerificationRequired() SignupRequest

	// Disables automatic cleanup of the UserSignup resource after the test has completed
	DisableCleanup() SignupRequest
}

func NewSignupRequest(t *testing.T, awaitilities wait.Awaitilities) SignupRequest {
	defaultUsername := fmt.Sprintf("testuser-%s", uuid.Must(uuid.NewV4()).String())
	return &signupRequest{
		t:                  t,
		awaitilities:       awaitilities,
		requiredHTTPStatus: http.StatusAccepted,
		username:           defaultUsername,
		email:              fmt.Sprintf("%s@test.com", defaultUsername),
	}
}

type signupRequest struct {
	t                    *testing.T
	awaitilities         wait.Awaitilities
	ensureMUR            bool
	manuallyApprove      bool
	verificationRequired bool
	identityID           *uuid.UUID
	username             string
	email                string
	requiredHTTPStatus   int
	targetCluster        *wait.MemberAwaitility
	conditions           []toolchainv1alpha1.Condition
	userSignup           *toolchainv1alpha1.UserSignup
	mur                  *toolchainv1alpha1.MasterUserRecord
	originalSub          string
	cleanupDisabled      bool
}

func (r *signupRequest) IdentityID(id uuid.UUID) SignupRequest {
	value := id
	r.identityID = &value
	return r
}

func (r *signupRequest) Username(username string) SignupRequest {
	r.username = username
	return r
}

func (r *signupRequest) Email(email string) SignupRequest {
	r.email = email
	return r
}

func (r *signupRequest) OriginalSub(originalSub string) SignupRequest {
	r.originalSub = originalSub
	return r
}

func (r *signupRequest) Resources() (*toolchainv1alpha1.UserSignup, *toolchainv1alpha1.MasterUserRecord) {
	return r.userSignup, r.mur
}

func (r *signupRequest) EnsureMUR() SignupRequest {
	r.ensureMUR = true
	return r
}

func (r *signupRequest) ManuallyApprove() SignupRequest {
	r.manuallyApprove = true
	return r
}

func (r *signupRequest) RequireConditions(conditions ...toolchainv1alpha1.Condition) SignupRequest {
	r.conditions = conditions
	return r
}

func (r *signupRequest) VerificationRequired() SignupRequest {
	r.verificationRequired = true
	return r
}

func (r *signupRequest) TargetCluster(targetCluster *wait.MemberAwaitility) SignupRequest {
	r.targetCluster = targetCluster
	return r
}

func (r *signupRequest) RequireHTTPStatus(httpStatus int) SignupRequest {
	r.requiredHTTPStatus = httpStatus
	return r
}

func (r *signupRequest) DisableCleanup() SignupRequest {
	r.cleanupDisabled = true
	return r
}

func (r *signupRequest) Execute() SignupRequest {
	hostAwait := r.awaitilities.Host()
	WaitUntilBaseNSTemplateTierIsUpdated(r.t, r.awaitilities.Host())

	var identityID uuid.UUID
	if r.identityID != nil {
		identityID = *r.identityID
	} else {
		identityID = uuid.Must(uuid.NewV4())
	}

	// Create a token and identity to sign up with
	userIdentity := &authsupport.Identity{
		ID:       identityID,
		Username: r.username,
	}

	claims := []authsupport.ExtraClaim{authsupport.WithEmailClaim(r.email)}
	if r.originalSub != "" {
		claims = append(claims, authsupport.WithOriginalSubClaim(r.originalSub))
	}
	token0, err := authsupport.GenerateSignedE2ETestToken(*userIdentity, claims...)
	require.NoError(r.t, err)

	// Call the signup endpoint
	invokeEndpoint(r.t, "POST", hostAwait.RegistrationServiceURL+"/api/v1/signup",
		token0, "", r.requiredHTTPStatus)

	// Wait for the UserSignup to be created
	userSignup, err := hostAwait.WaitForUserSignup(userIdentity.ID.String())
	require.NoError(r.t, err)

	if r.targetCluster != nil && hostAwait.GetToolchainConfig().Spec.Host.AutomaticApproval.Enabled != nil {
		require.False(r.t, *hostAwait.GetToolchainConfig().Spec.Host.AutomaticApproval.Enabled,
			"cannot specify a target cluster for new signup requests while automatic approval is enabled")
	}

	if r.manuallyApprove || r.targetCluster != nil || (r.verificationRequired != states.VerificationRequired(userSignup)) {
		doUpdate := func(instance *toolchainv1alpha1.UserSignup) {
			// We set the VerificationRequired state first, because if manuallyApprove is also set then it will
			// reset the VerificationRequired state to false.
			if r.verificationRequired != states.VerificationRequired(instance) {
				states.SetVerificationRequired(userSignup, r.verificationRequired)
			}

			if r.manuallyApprove {
				states.SetApproved(instance, r.manuallyApprove)
			}
			if r.targetCluster != nil {
				instance.Spec.TargetCluster = r.targetCluster.ClusterName
			}
		}

		userSignup, err = hostAwait.UpdateUserSignup(userSignup.Name, doUpdate)
		require.NoError(r.t, err)
	}

	r.t.Logf("user signup '%s' created", userSignup.Name)

	// If any required conditions have been specified, confirm the UserSignup has them
	if len(r.conditions) > 0 {
		userSignup, err = hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(r.conditions...))
		require.NoError(r.t, err)
	}

	r.userSignup = userSignup

	if r.ensureMUR {
		expectedTier := "base"
		if hostAwait.GetToolchainConfig().Spec.Host.Tiers.DefaultTier != nil {
			expectedTier = *hostAwait.GetToolchainConfig().Spec.Host.Tiers.DefaultTier
		}
		VerifyResourcesProvisionedForSignup(r.t, r.awaitilities, userSignup, expectedTier)
		mur, err := hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
		require.NoError(r.t, err)
		r.mur = mur
	}

	// We also need to ensure that the UserSignup is deleted at the end of the test (if the test itself doesn't delete it)
	// and if cleanup hasn't been disabled
	if !r.cleanupDisabled {
		hostAwait.Cleanup(r.userSignup)
	}

	return r
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
	req.Close = true
	resp, err := httpClient.Do(req)

	require.NoError(t, err, "error posting signup request.\nmethod : %s\npath : %s\nauthToken : %s\nbody : %s", method, path, authToken, requestBody)

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

func Close(t *testing.T, resp *http.Response) {
	_, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	err = resp.Body.Close()
	require.NoError(t, err)
}
