package sandboxui

import (
	"context"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	t.Log("Deleting UserSignup")

	if err := hostAwait.Client.Delete(context.TODO(), userSignup); err != nil {
		if !errors.IsNotFound(err) {
			return err
		}
	}

	// Wait until deletion is complete
	t.Log("Waiting until UserSignup is completely deleted")
	return hostAwait.WaitUntilUserSignupDeleted(t, username)
}

func GetUserSignupWithClient(t *testing.T, client client.Client, username string) *toolchainv1alpha1.UserSignup {
	t.Logf("Getting UserSignup")

	userSignup := &toolchainv1alpha1.UserSignup{}
	err := client.Get(context.TODO(), types.NamespacedName{
		Namespace: configuration.DefaultHostNS,
		Name:      username,
	}, userSignup)
	if errors.IsNotFound(err) {
		return nil
	}
	require.NoError(t, err)

	return userSignup
}

func DeleteUserSignupWithClient(t *testing.T, client client.Client, userSignup *toolchainv1alpha1.UserSignup) {
	t.Log("Deleting UserSignup")

	err := client.Delete(context.TODO(), userSignup)
	require.NoError(t, err)

	// Wait until UserSignup is actually deleted
	t.Log("Waiting for UserSignup to be completely deleted")
	err = k8swait.PollUntilContextTimeout(context.TODO(), time.Second, 2*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			userSignup := GetUserSignupWithClient(t, client, userSignup.Name)
			if userSignup == nil {
				t.Log("UserSignup successfully deleted")
				return true, nil
			}
			// Still exists
			return false, nil
		})
	require.NoError(t, err, "UserSignup was not deleted within timeout")

	t.Log("UserSignup deleted")
}
