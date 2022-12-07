package testsupport

import (
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/hash"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/gofrs/uuid"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// CreateBannedUser creates the BannedUser resource
func CreateBannedUser(t *testing.T, hostAwait *wait.HostAwaitility, email string) *toolchainv1alpha1.BannedUser {
	bannedUser := NewBannedUser(hostAwait, email)
	hostAwait.CreateWithCleanup(t, bannedUser)

	t.Logf("BannedUser '%s' created", bannedUser.Spec.Email)
	return bannedUser
}

// NewBannedUser initializes a new BannedUser object
func NewBannedUser(host *wait.HostAwaitility, email string) *toolchainv1alpha1.BannedUser {
	return &toolchainv1alpha1.BannedUser{
		ObjectMeta: metav1.ObjectMeta{
			Name:      uuid.Must(uuid.NewV4()).String(),
			Namespace: host.Namespace,
			Labels: map[string]string{
				toolchainv1alpha1.BannedUserEmailHashLabelKey: hash.EncodeString(email),
			},
		},
		Spec: toolchainv1alpha1.BannedUserSpec{
			Email: email,
		},
	}
}
