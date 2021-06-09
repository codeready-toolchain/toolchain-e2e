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

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
)

var httpClient = HTTPClient

// UserRequest
type UserRequest interface {
	Conditions(conditions ...toolchainv1alpha1.Condition) UserRequest
	Email(email string) UserRequest
	EnsureMUR() UserRequest
	Execute() UserRequest
	ManuallyApprove() UserRequest
	RequireHttpStatus(httpStatus uint) UserRequest
	Resources() (*toolchainv1alpha1.UserSignup, *toolchainv1alpha1.MasterUserRecord)
	TargetCluster(targetCluster *wait.MemberAwaitility) UserRequest
	Username(username string) UserRequest
	VerificationRequired() UserRequest
}

func NewUserRequest(t *testing.T, hostAwait *wait.HostAwaitility, memberAwait *wait.MemberAwaitility, member2Await *wait.MemberAwaitility) UserRequest {
	return &userRequest{
		t:                  t,
		hostAwait:          hostAwait,
		memberAwait:        memberAwait,
		member2Await:       member2Await,
		requiredHttpStatus: http.StatusAccepted,
	}
}

type userRequest struct {
	t                    *testing.T
	hostAwait            *wait.HostAwaitility
	memberAwait          *wait.MemberAwaitility
	member2Await         *wait.MemberAwaitility
	ensureMUR            bool
	manuallyApprove      bool
	verificationRequired bool
	username             *string
	email                *string
	requiredHttpStatus   uint
	targetCluster        *wait.MemberAwaitility
	conditions           []toolchainv1alpha1.Condition
	userSignup           *toolchainv1alpha1.UserSignup
	mur                  *toolchainv1alpha1.MasterUserRecord
}

func (r *userRequest) Username(username string) UserRequest {
	r.username = &username
	return r
}

func (r *userRequest) Email(email string) UserRequest {
	r.email = &email
	return r
}

func (r *userRequest) Resources() (*toolchainv1alpha1.UserSignup, *toolchainv1alpha1.MasterUserRecord) {
	return r.userSignup, r.mur
}

func (r *userRequest) EnsureMUR() UserRequest {
	r.ensureMUR = true
	return r
}

func (r *userRequest) ManuallyApprove() UserRequest {
	r.manuallyApprove = true
	return r
}

func (r *userRequest) Conditions(conditions ...toolchainv1alpha1.Condition) UserRequest {
	r.conditions = conditions
	return r
}

func (r *userRequest) VerificationRequired() UserRequest {
	r.verificationRequired = true
	return r
}

func (r *userRequest) TargetCluster(targetCluster *wait.MemberAwaitility) UserRequest {
	r.targetCluster = targetCluster
	return r
}

func (r *userRequest) RequireHttpStatus(httpStatus uint) UserRequest {
	r.requiredHttpStatus = httpStatus
	return r
}

func (r *userRequest) Execute() UserRequest {
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
		token0, "", r.requiredHttpStatus)

	// Wait for the UserSignup to be created
	userSignup, err := r.hostAwait.WaitForUserSignup(userIdentity.ID.String())
	require.NoError(r.t, err)

	if r.manuallyApprove || r.targetCluster != nil || (r.verificationRequired != states.VerificationRequired(userSignup)) {
		// We set the VerificationRequired state first, because if manuallyApprove is also set then it will
		// reset the VerificationRequired state to false.
		if r.verificationRequired != states.VerificationRequired(userSignup) {
			states.SetVerificationRequired(userSignup, r.verificationRequired)
		}

		if r.manuallyApprove {
			states.SetApproved(userSignup, r.manuallyApprove)
		}
		if r.targetCluster != nil {
			userSignup.Spec.TargetCluster = r.targetCluster.ClusterName
		}
		err = r.hostAwait.FrameworkClient.Update(context.TODO(), userSignup)
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

	return r
}

func invokeEndpoint(t *testing.T, method, path, authToken, requestBody string, requiredStatus uint) map[string]interface{} {
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
