package sandboxui

import (
	"context"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
)

// GetUserSignup retrieves the UserSignup for the configured SSO username
func GetUserSignup(t *testing.T, hostAwait *wait.HostAwaitility, username string) *toolchainv1alpha1.UserSignup {
	var userSignup *toolchainv1alpha1.UserSignup
	err := k8swait.PollUntilContextTimeout(context.TODO(), time.Second, 2*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			us := &toolchainv1alpha1.UserSignup{}
			if err := hostAwait.Client.Get(ctx,
				types.NamespacedName{Name: username, Namespace: hostAwait.Namespace},
				us); err != nil {
				if errors.IsNotFound(err) {
					return false, nil // Keep polling
				}
				return false, err
			}
			userSignup = us
			return true, nil
		})

	if err != nil {
		t.Logf("UserSignup %s not found after timeout", username)
		return nil
	}

	return userSignup
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
	return k8swait.PollUntilContextTimeout(context.TODO(), time.Second, 2*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			us := &toolchainv1alpha1.UserSignup{}
			if err := hostAwait.Client.Get(ctx, types.NamespacedName{Name: username, Namespace: hostAwait.Namespace}, us); err != nil {
				if errors.IsNotFound(err) {
					t.Logf("UserSignup %s successfully deleted", username)
					return true, nil
				}
				return false, err
			}
			return false, nil // Still exists, keep polling
		})
}
