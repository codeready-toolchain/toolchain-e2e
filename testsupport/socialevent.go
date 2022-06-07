package testsupport

import (
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewSocialEvent returns a new SocialEvent with the given name and tiername,
// starting: now
// ending: 1hr later
// max attendees: 10
func NewSocialEvent(namespace, name, userTier, spaceTier string) *toolchainv1alpha1.SocialEvent {
	se := &toolchainv1alpha1.SocialEvent{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Spec: toolchainv1alpha1.SocialEventSpec{
			UserTier:     userTier,
			SpaceTier:    spaceTier,
			StartTime:    metav1.Now(),
			EndTime:      metav1.NewTime(time.Now().Add(1 * time.Hour)),
			MaxAttendees: 10,
		},
	}
	return se
}
