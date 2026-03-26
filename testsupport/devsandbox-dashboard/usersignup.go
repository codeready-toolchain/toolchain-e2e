package sandboxui

import (
	"context"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"k8s.io/apimachinery/pkg/api/errors"
)

// WaitForUserSignup retrieves the UserSignup for the configured SSO username
func WaitForUserSignup(t *testing.T, hostAwait *wait.HostAwaitility, username string) (*toolchainv1alpha1.UserSignup, error) {
	return hostAwait.WithRetryOptions(wait.TimeoutOption(time.Minute*2)).WaitForUserSignup(t, username,
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedAutomatically())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueApproved))
}

// DeleteUserSignup deletes the UserSignup for the configured SSO username
// and waits until it's fully removed from the cluster
func DeleteUserSignup(t *testing.T, hostAwait *wait.HostAwaitility, userSignup *toolchainv1alpha1.UserSignup) error {
	username := userSignup.Name

	t.Logf("Deleting UserSignup %s", username)

	if err := hostAwait.Client.Delete(context.TODO(), userSignup); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	}

	// Wait until deletion is complete
	t.Logf("Waiting until UserSignup %s is completely deleted", username)
	return hostAwait.WaitUntilUserSignupDeleted(t, username)
}
