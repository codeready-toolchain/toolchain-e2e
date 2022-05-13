package testsupport

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	commonauth "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	authsupport "github.com/codeready-toolchain/toolchain-e2e/testsupport/auth"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/cleanup"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
)

var httpClient = HTTPClient

// NewSignupRequest creates a new signup request for the registration service
func NewSignupRequest(t *testing.T, awaitilities wait.Awaitilities) *SignupRequest {
	defaultUsername := fmt.Sprintf("testuser-%s", uuid.Must(uuid.NewV4()).String())
	return &SignupRequest{
		t:                  t,
		awaitilities:       awaitilities,
		requiredHTTPStatus: http.StatusAccepted,
		username:           defaultUsername,
		email:              fmt.Sprintf("%s@test.com", defaultUsername),
		identityID:         uuid.Must(uuid.NewV4()),
	}
}

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
type SignupRequest struct {
	t                    *testing.T
	awaitilities         wait.Awaitilities
	ensureMUR            bool
	waitForMUR           bool
	manuallyApprove      bool
	verificationRequired bool
	identityID           uuid.UUID
	username             string
	email                string
	requiredHTTPStatus   int
	targetCluster        *wait.MemberAwaitility
	conditions           []toolchainv1alpha1.Condition
	userSignup           *toolchainv1alpha1.UserSignup
	mur                  *toolchainv1alpha1.MasterUserRecord
	token                string
	originalSub          string
	cleanupDisabled      bool
	noSpace              bool
}

// IdentityID specifies the ID value for the user's Identity.  This value if set will be used to set both the
// "Subject" and "IdentityID" claims in the user's auth token.  If not set, a new UUID value will be used
func (r *SignupRequest) IdentityID(id uuid.UUID) *SignupRequest {
	value := id
	r.identityID = value
	return r
}

// Username specifies the username of the user
func (r *SignupRequest) Username(username string) *SignupRequest {
	r.username = username
	return r
}

// Email specifies the email address to use for the new UserSignup
func (r *SignupRequest) Email(email string) *SignupRequest {
	r.email = email
	return r
}

// OriginalSub specifies the original sub value which will be used for migrating the user to a new IdP client
func (r *SignupRequest) OriginalSub(originalSub string) *SignupRequest {
	r.originalSub = originalSub
	return r
}

// Resources may be called only after a call to Execute().  It returns two parameters; the first is the UserSignup
// instance that was created, the second is the MasterUserRecord instance, HOWEVER the MUR will only be returned
// here if EnsureMUR() was also called previously, otherwise a nil value will be returned
func (r *SignupRequest) Resources() (*toolchainv1alpha1.UserSignup, *toolchainv1alpha1.MasterUserRecord) {
	return r.userSignup, r.mur
}

// EnsureMUR will ensure that a MasterUserRecord is created.  It is necessary to call this function in order for
// the Resources() function to return a non-nil value for its second return parameter.
func (r *SignupRequest) EnsureMUR() *SignupRequest {
	r.ensureMUR = true
	return r
}

// WaitForMUR will wait until MasterUserRecord is created
func (r *SignupRequest) WaitForMUR() *SignupRequest {
	r.waitForMUR = true
	return r
}

// GetToken may be called only after a call to Execute(). It returns the token that was generated for the request
func (r *SignupRequest) GetToken() string {
	return r.token
}

// ManuallyApprove if called will set the "approved" state to true after the UserSignup has been created
func (r *SignupRequest) ManuallyApprove() *SignupRequest {
	r.manuallyApprove = true
	return r
}

// RequireConditions specifies the condition values that the new UserSignup is required to have in order for
// the signup to be considered successful
func (r *SignupRequest) RequireConditions(conditions ...toolchainv1alpha1.Condition) *SignupRequest {
	r.conditions = conditions
	return r
}

// VerificationRequired specifies that the "verification-required" state will be set for the new UserSignup, however
// if ManuallyApprove() is also called then this will have no effect as user approval overrides the verification
// required state.
func (r *SignupRequest) VerificationRequired() *SignupRequest {
	r.verificationRequired = true
	return r
}

// TargetCluster may be provided in order to specify the user's target cluster
func (r *SignupRequest) TargetCluster(targetCluster *wait.MemberAwaitility) *SignupRequest {
	r.targetCluster = targetCluster
	return r
}

// RequireHTTPStatus may be used to override the expected HTTP response code received from the Registration Service.
// If not specified, here, the default expected value is StatusAccepted
func (r *SignupRequest) RequireHTTPStatus(httpStatus int) *SignupRequest {
	r.requiredHTTPStatus = httpStatus
	return r
}

// DisableCleanup disables automatic cleanup of the UserSignup resource after the test has completed
func (r *SignupRequest) DisableCleanup() *SignupRequest {
	r.cleanupDisabled = true
	return r
}

// NoSpace creates only a UserSignup and MasterUserRecord, Space creation will be skipped
func (r *SignupRequest) NoSpace() *SignupRequest {
	r.noSpace = true
	return r
}

var usernamesInParallel = &namesRegistry{usernames: map[string]string{}}

type namesRegistry struct {
	sync.RWMutex
	usernames map[string]string
}

func (r *namesRegistry) add(t *testing.T, name string) {
	r.Lock()
	defer r.Unlock()
	pwd := os.Getenv("PWD")
	if !strings.HasSuffix(pwd, "parallel") {
		return
	}
	if testName, exist := r.usernames[name]; exist {
		require.Fail(t, fmt.Sprintf("The username '%s' was already used in the test '%s'", name, testName))
	}
	r.usernames[name] = t.Name()
}

// Execute executes the request against the Registration service REST endpoint.  This function may only be called
// once, and must be called after all other functions EXCEPT for Resources()
func (r *SignupRequest) Execute() *SignupRequest {
	hostAwait := r.awaitilities.Host()
	err := hostAwait.WaitUntilBaseNSTemplateTierIsUpdated()
	require.NoError(r.t, err)

	// Create a token and identity to sign up with
	usernamesInParallel.add(r.t, r.username)

	userIdentity := &commonauth.Identity{
		ID:       r.identityID,
		Username: r.username,
	}
	claims := []commonauth.ExtraClaim{commonauth.WithEmailClaim(r.email)}
	if r.originalSub != "" {
		claims = append(claims, commonauth.WithOriginalSubClaim(r.originalSub))
	}
	r.token, err = authsupport.NewTokenFromIdentity(userIdentity, claims...)
	require.NoError(r.t, err)

	queryParams := map[string]string{}
	if r.noSpace {
		queryParams["no-space"] = "true"
	}

	// Call the signup endpoint
	invokeEndpoint(r.t, "POST", hostAwait.RegistrationServiceURL+"/api/v1/signup",
		r.token, "", r.requiredHTTPStatus, queryParams)

	// Wait for the UserSignup to be created
	//userSignup, err := hostAwait.WaitForUserSignup(userIdentity.Username)
	// TODO remove this after reg service PR #254 is merged
	userSignup, err := hostAwait.WaitForUserSignupByUserIDAndUsername(userIdentity.ID.String(), userIdentity.Username)

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

	if r.waitForMUR {
		mur, err := hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
		require.NoError(r.t, err)
		r.mur = mur
	}

	if r.ensureMUR {
		expectedSpaceTier := "base"
		if hostAwait.GetToolchainConfig().Spec.Host.Tiers.DefaultTier != nil {
			expectedSpaceTier = *hostAwait.GetToolchainConfig().Spec.Host.Tiers.DefaultSpaceTier
		}
		VerifyResourcesProvisionedForSignup(r.t, r.awaitilities, userSignup, "deactivate30", expectedSpaceTier)
		mur, err := hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
		require.NoError(r.t, err)
		r.mur = mur
	}

	// We also need to ensure that the UserSignup is deleted at the end of the test (if the test itself doesn't delete it)
	// and if cleanup hasn't been disabled
	if !r.cleanupDisabled {
		cleanup.AddCleanTasks(hostAwait, r.userSignup)
	}

	return r
}

func invokeEndpoint(t *testing.T, method, path, authToken, requestBody string, requiredStatus int, queryParams map[string]string) map[string]interface{} {
	var reqBody io.Reader
	if requestBody != "" {
		reqBody = strings.NewReader(requestBody)
	}
	req, err := http.NewRequest(method, path, reqBody)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+authToken)
	req.Header.Set("content-type", "application/json")

	if len(queryParams) > 0 {
		q := req.URL.Query()
		for key, val := range queryParams {
			q.Add(key, val)
		}
		req.URL.RawQuery = q.Encode()
	}

	req.Close = true
	resp, err := httpClient.Do(req) // nolint:bodyclose // see `defer Close(t, resp)`
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
	if resp == nil {
		return
	}
	_, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	err = resp.Body.Close()
	require.NoError(t, err)
}
