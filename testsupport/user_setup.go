package testsupport

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	authsupport "github.com/codeready-toolchain/toolchain-common/pkg/test/auth"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/gofrs/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func CreateMultipleSignups(t *testing.T, awaitilities wait.Awaitilities, targetCluster *wait.MemberAwaitility, capacity int) []*toolchainv1alpha1.UserSignup {
	hostAwait := awaitilities.Host()
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
		signups[i], _ = NewSignupRequest(t, awaitilities).
			Username(name).
			Email(fmt.Sprintf("multiple-signup-testuser-%d@test.com", i)).
			ManuallyApprove().
			TargetCluster(targetCluster).
			Execute().
			Resources()
	}
	return signups
}

type IdentityOption func(*authsupport.Identity) error

// NewUserSignup creates a new UserSignup resoruce with the given values:
// specApproved defines if the UserSignup should be manually approved
// username defines the required username set in the spec
// email is set in "user-email" annotation
// setTargetCluster defines if the UserSignup will be created with Spec.TargetCluster set to the first found member cluster name
func NewUserSignup(namespace, username string, email string) *toolchainv1alpha1.UserSignup {
	name := uuid.Must(uuid.NewV4()).String()
	return &toolchainv1alpha1.UserSignup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
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
			InsecureSkipVerify: true, // nolint:gosec
		},
	},
}
