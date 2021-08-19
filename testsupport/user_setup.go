package testsupport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func CreateMultipleSignups(t *testing.T, hostAwait *wait.HostAwaitility, targetCluster *wait.MemberAwaitility, capacity int) []*toolchainv1alpha1.UserSignup {
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
		states.SetApproved(userSignup, true)
		if targetCluster != nil {
			userSignup.Spec.TargetCluster = targetCluster.ClusterName
		}
		err := hostAwait.CreateWithCleanup(context.TODO(), userSignup)
		hostAwait.T.Logf("created usersignup with username: '%s' and resource name: '%s'", userSignup.Spec.Username, userSignup.Name)
		require.NoError(t, err)
		signups[i] = userSignup
	}
	return signups
}

type IdentityOption func(*authsupport.Identity) error

// NewUserSignup creates a new UserSignup resoruce with the given values:
// specApproved defines if the UserSignup should be manually approved
// username defines the required username set in the spec
// email is set in "user-email" annotation
// setTargetCluster defines if the UserSignup will be created with Spec.TargetCluster set to the first found member cluster name
func NewUserSignup(t *testing.T, hostAwait *wait.HostAwaitility, username string, email string) *toolchainv1alpha1.UserSignup {
	WaitUntilBaseNSTemplateTierIsUpdated(t, hostAwait)

	name := uuid.Must(uuid.NewV4()).String()

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
			Userid:   name,
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

func ToIdentityName(userID string) string {
	return fmt.Sprintf("%s:%s", "rhd", userID)
}
