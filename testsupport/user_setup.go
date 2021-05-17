package testsupport

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	framework "github.com/operator-framework/operator-sdk/pkg/test"
	uuid "github.com/satori/go.uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func CreateMultipleSignups(t *testing.T, ctx *framework.Context, hostAwait *wait.HostAwaitility, targetCluster *wait.MemberAwaitility, capacity int) []*toolchainv1alpha1.UserSignup {
	signups := make([]*toolchainv1alpha1.UserSignup, capacity)
	for i := 0; i < capacity; i++ {
		name := fmt.Sprintf("multiple-signup-testuser-%d", i)
		// check if there is already a MUR with the expected name, in which case, continue with the next one
		mur := toolchainv1alpha1.MasterUserRecord{}
		if err := hostAwait.Client.Get(context.TODO(), types.NamespacedName{Namespace: hostAwait.Namespace, Name: name}, &mur); err == nil {
			t.Logf("no need to create a UserSignup for '%s', the MasterUserRecord resource already exists", name)
			// skip this one, it already exists
			continue
		}
		// Create an approved UserSignup resource
		userSignup := NewUserSignup(t, hostAwait, name, fmt.Sprintf("multiple-signup-testuser-%d@test.com", i))
		userSignup.Spec.Approved = true
		if targetCluster != nil {
			userSignup.Spec.TargetCluster = targetCluster.ClusterName
		}
		err := hostAwait.FrameworkClient.Create(context.TODO(), userSignup, CleanupOptions(ctx))
		hostAwait.T.Logf("created usersignup with username: '%s' and resource name: '%s'", userSignup.Spec.Username, userSignup.Name)
		require.NoError(t, err)
		signups[i] = userSignup
	}
	return signups
}

type IdentityOption func(*authsupport.Identity) error

func WithIdentityID(idStr string) IdentityOption {
	return func(identity *authsupport.Identity) error {
		id, err := uuid.FromString(idStr)
		if err != nil {
			return err
		}
		identity.ID = id
		return nil
	}
}

func CreateAndApproveSignup(t *testing.T, hostAwait *wait.HostAwaitility, username, targetCluster string, options ...IdentityOption) *toolchainv1alpha1.UserSignup {
	WaitUntilBaseNSTemplateTierIsUpdated(t, hostAwait)
	// 1. Create a UserSignup resource via calling registration service
	identity := &authsupport.Identity{
		ID:       uuid.NewV4(),
		Username: username,
	}
	for _, apply := range options {
		err := apply(identity)
		require.NoError(t, err)
	}
	postSignup(t, hostAwait.RegistrationServiceURL, *identity)

	// at this stage, the usersignup should not be approved nor completed
	userSignup, err := hostAwait.WaitForUserSignup(identity.ID.String(),
		wait.UntilUserSignupHasConditions(PendingApproval()...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValuePending))
	require.NoError(t, err)
	require.Equal(t, identity.Username+"-First-Name", userSignup.Spec.GivenName)
	require.Equal(t, identity.Username+"-Last-Name", userSignup.Spec.FamilyName)
	require.Equal(t, identity.Username+"-Company-Name", userSignup.Spec.Company)
	require.Equal(t, identity.Username+"@acme.com", userSignup.Annotations[toolchainv1alpha1.UserSignupUserEmailAnnotationKey])

	// 2. approve the UserSignup
	userSignup.Spec.TargetCluster = targetCluster
	userSignup.Spec.Approved = true
	err = hostAwait.Client.Update(context.TODO(), userSignup)
	require.NoError(t, err)
	// Check the updated conditions
	userSignup, err = hostAwait.WaitForUserSignup(userSignup.Name,
		wait.UntilUserSignupHasConditions(ApprovedByAdmin()...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
	require.NoError(t, err)

	// First, wait for the MasterUserRecord to exist, no matter what status
	mur, err := hostAwait.WaitForMasterUserRecord(userSignup.Status.CompliantUsername, wait.UntilMasterUserRecordHasConditions(Provisioned(), ProvisionedNotificationCRCreated()))
	require.NoError(t, err)
	// check that there's an annotation with the user's email address
	assert.Equal(t, identity.Username+"@acme.com", mur.Annotations[toolchainv1alpha1.MasterUserRecordEmailAnnotationKey]) // same as on userSignup

	// Wait for the the notification CR to be created & sent
	notifications, err := hostAwait.WaitForNotifications(mur.Name, toolchainv1alpha1.NotificationTypeProvisioned, 1, wait.UntilNotificationHasConditions(Sent()))
	require.NoError(t, err)
	require.NotEmpty(t, notifications)
	for _, notification := range notifications {
		assert.Contains(t, notification.Name, mur.Name+"-provisioned-")
		assert.Equal(t, mur.Namespace, notification.Namespace)
		assert.Equal(t, "userprovisioned", notification.Spec.Template)
		assert.Equal(t, mur.Spec.UserID, notification.Spec.UserID)
	}

	err = hostAwait.WaitUntilNotificationsDeleted(mur.Name, toolchainv1alpha1.NotificationTypeProvisioned)
	require.NoError(t, err)

	// delete the userSignup at the end of the test
	t.Cleanup(func() {
		if err := hostAwait.Client.Delete(context.TODO(), userSignup); err != nil && !errors.IsNotFound(err) {
			require.NoError(t, err)
		}
	})
	return userSignup
}

// NewUserSignup creates a new UserSignup resoruce with the given values:
// specApproved defines if the UserSignup should be manually approved
// username defines the required username set in the spec
// email is set in "user-email" annotation
// setTargetCluster defines if the UserSignup will be created with Spec.TargetCluster set to the first found member cluster name
func NewUserSignup(t *testing.T, hostAwait *wait.HostAwaitility, username string, email string) *toolchainv1alpha1.UserSignup {
	WaitUntilBaseNSTemplateTierIsUpdated(t, hostAwait)

	name := uuid.NewV4().String()

	return &toolchainv1alpha1.UserSignup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: hostAwait.Namespace,
			Annotations: map[string]string{
				toolchainv1alpha1.UserSignupUserEmailAnnotationKey: email,
			},
			Labels: map[string]string{
				toolchainv1alpha1.UserSignupUserEmailHashLabelKey: md5.CalcMd5(email),
			},
		},
		Spec: toolchainv1alpha1.UserSignupSpec{
			Username: username,
			UserID:   name,
		},
	}
}

var HTTPClient = &http.Client{
	Timeout: time.Second * 10,
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	},
}

func postSignup(t *testing.T, route string, identity authsupport.Identity) {
	require.NotEmpty(t, route)
	// Call signup endpoint with a valid token.
	emailClaim := authsupport.WithEmailClaim(identity.Username + "@acme.com")
	givenNameClaim := authsupport.WithGivenNameClaim(identity.Username + "-First-Name")
	familyNameClaim := authsupport.WithFamilyNameClaim(identity.Username + "-Last-Name")
	companyClaim := authsupport.WithCompanyClaim(identity.Username + "-Company-Name")
	iatClaim := authsupport.WithIATClaim(time.Now().Add(-60 * time.Second))
	token, err := authsupport.GenerateSignedE2ETestToken(identity, emailClaim, companyClaim, givenNameClaim, familyNameClaim, iatClaim)
	require.NoError(t, err)

	req, err := http.NewRequest("POST", route+"/api/v1/signup", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("content-type", "application/json")
	client := HTTPClient
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() {
		err := resp.Body.Close()
		require.NoError(t, err)
	}()
	r, err := ioutil.ReadAll(resp.Body)
	require.NoError(t, err)
	require.True(t, resp.StatusCode < 300, "unexpected status code after posting user signup: '%d' ('%s')", resp.StatusCode, string(r))
}

func ToIdentityName(userID string) string {
	return fmt.Sprintf("%s:%s", "rhd", userID)
}
