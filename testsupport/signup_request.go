package testsupport

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"

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

	// Username specifies the username of the user
	Username(username string) SignupRequest

	// VerificationRequired specifies that the "verification-required" state will be set for the new UserSignup, however
	// if ManuallyApprove() is also called then this will have no effect as user approval overrides the verification
	// required state.
	VerificationRequired() SignupRequest
}

func NewSignupRequest(t *testing.T, hostAwait *wait.HostAwaitility, memberAwait *wait.MemberAwaitility, member2Await *wait.MemberAwaitility) SignupRequest {
	return &signupRequest{
		t:                  t,
		hostAwait:          hostAwait,
		memberAwait:        memberAwait,
		member2Await:       member2Await,
		requiredHTTPStatus: http.StatusAccepted,
	}
}

type signupRequest struct {
	t                    *testing.T
	hostAwait            *wait.HostAwaitility
	memberAwait          *wait.MemberAwaitility
	member2Await         *wait.MemberAwaitility
	ensureMUR            bool
	manuallyApprove      bool
	verificationRequired bool
	username             *string
	email                *string
	requiredHTTPStatus   int
	targetCluster        *wait.MemberAwaitility
	conditions           []toolchainv1alpha1.Condition
	userSignup           *toolchainv1alpha1.UserSignup
	mur                  *toolchainv1alpha1.MasterUserRecord
}

func (r *signupRequest) Username(username string) SignupRequest {
	r.username = &username
	return r
}

func (r *signupRequest) Email(email string) SignupRequest {
	r.email = &email
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

func (r *signupRequest) Execute() SignupRequest {
	WaitUntilBaseNSTemplateTierIsUpdated(r.t, r.hostAwait)

	var username string
	if r.username != nil {
		username = *r.username
	} else {
		username = fmt.Sprintf("testuser-%s", uuid.Must(uuid.NewV4()).String())
	}

	// Create a token and identity to sign up with
	userIdentity := &authsupport.Identity{
		ID:       uuid.Must(uuid.NewV4()),
		Username: username,
	}

	var email string
	if r.email != nil {
		email = *r.email
	} else {
		email = fmt.Sprintf("%s@test.com", username)
	}

	emailClaim0 := authsupport.WithEmailClaim(email)
	token0, err := authsupport.GenerateSignedE2ETestToken(*userIdentity, emailClaim0)
	require.NoError(r.t, err)

	// Call the signup endpoint
	invokeEndpoint(r.t, "POST", r.hostAwait.RegistrationServiceURL+"/api/v1/signup",
		token0, "", r.requiredHTTPStatus)

	// Wait for the UserSignup to be created
	userSignup, err := r.hostAwait.WaitForUserSignup(userIdentity.ID.String())
	require.NoError(r.t, err)

	if r.targetCluster != nil {
		if r.hostAwait.GetToolchainConfig().Spec.Host.AutomaticApproval.Enabled != nil {
			require.False(r.t, *r.hostAwait.GetToolchainConfig().Spec.Host.AutomaticApproval.Enabled,
				"cannot specify a target cluster for new signup requests while automatic approval is enabled")
		}
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

		userSignup, err = r.hostAwait.UpdateUserSignupSpec(userSignup.Name, doUpdate)
		require.NoError(r.t, err)
	}

	r.t.Logf("user signup '%s' created", userSignup.Name)

	// If any required conditions have been specified, confirm the UserSignup has them
	if len(r.conditions) > 0 {
		userSignup, err = r.hostAwait.WaitForUserSignup(userSignup.Name, wait.UntilUserSignupHasConditions(r.conditions...))
		require.NoError(r.t, err)
	}

	r.userSignup = userSignup

	if r.ensureMUR {
		// Confirm the MUR was created and ready
		VerifyResourcesProvisionedForSignup(r.t, r.hostAwait, userSignup, "base", r.memberAwait, r.member2Await)
		mur, err := r.hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername)
		require.NoError(r.t, err)
		r.mur = mur
	}

	// We also need to ensure that the UserSignup is deleted at the end of the test (if the test itself doesn't delete it)
	r.t.Cleanup(func() {
		if err := r.hostAwait.Client.Delete(context.TODO(), r.userSignup); err != nil && !errors.IsNotFound(err) {
			require.NoError(r.t, err)
		}
	})

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

func close(t *testing.T, resp *http.Response) {
	_, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	err = resp.Body.Close()
	require.NoError(t, err)
}
