package sandboxui

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/api/errors"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
)

const (
	configFlag = "--config"
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

func GetUserSignupThroughKsctl(t *testing.T, username string) *toolchainv1alpha1.UserSignup {
	t.Logf("Getting UserSignup through ksctl")

	// #nosec G204 -- username is from test config, not user input
	cmd := exec.Command("ksctl", "get", "usersignup", username, configFlag, viper.GetString("KUBECONFIG"), "-t", "host", "-o", "yaml")
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Check if it's a not found error
		if strings.Contains(string(output), "Error from server (NotFound)") {
			return nil
		}
	}
	require.NoError(t, err)

	// parse the output as a UserSignup
	userSignup := &toolchainv1alpha1.UserSignup{}
	err = yaml.Unmarshal(output, userSignup)
	require.NoError(t, err)

	return userSignup
}

func DeleteUserSignupThroughKsctl(t *testing.T, username string) {
	t.Log("Deleting UserSignup through ksctl")

	// #nosec G204 -- username is from test config, not user input
	cmd := exec.Command("ksctl", "gdpr-delete", username, configFlag, viper.GetString("KUBECONFIG"))
	cmd.Stdin = strings.NewReader("y\n") // confirm deletion
	output, err := cmd.CombinedOutput()
	require.NoError(t, err)
	require.Contains(t, string(output), "The deletion of the UserSignup has been triggered")

	// Wait until UserSignup is actually deleted
	t.Log("Waiting for UserSignup to be completely deleted")
	err = k8swait.PollUntilContextTimeout(context.TODO(), time.Second, 2*time.Minute, true,
		func(ctx context.Context) (done bool, err error) {
			// #nosec G204 -- username is from test config, not user input
			cmd := exec.Command("ksctl", "get", "usersignup", username, configFlag, viper.GetString("KUBECONFIG"), "-t", "host", "-o", "yaml")
			output, err := cmd.CombinedOutput()
			if err != nil {
				// Check if it's a not found error
				if strings.Contains(string(output), "not found") || strings.Contains(err.Error(), "not found") {
					t.Log("UserSignup successfully deleted")
					return true, nil
				}
				// Some other error, keep trying
				t.Log("Error checking UserSignup deletion")
				return false, nil
			}
			// Still exists
			return false, nil
		})
	require.NoError(t, err, "UserSignup was not deleted within timeout")

	t.Log("UserSignup deleted and confirmed through ksctl")
}
