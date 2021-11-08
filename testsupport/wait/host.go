package wait

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	testconfig "github.com/codeready-toolchain/toolchain-common/pkg/test/config"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"

	"github.com/ghodss/yaml"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// HostAwaitility the Awaitility for the Host cluster
type HostAwaitility struct {
	*Awaitility
	RegistrationServiceNs  string
	RegistrationServiceURL string
}

// NewHostAwaitility initializes a HostAwaitility
func NewHostAwaitility(t *testing.T, cfg *rest.Config, cl client.Client, ns string, registrationServiceNs string) *HostAwaitility {
	return &HostAwaitility{
		Awaitility: &Awaitility{
			T:             t,
			Client:        cl,
			RestConfig:    cfg,
			Namespace:     ns,
			Type:          cluster.Host,
			RetryInterval: DefaultRetryInterval,
			Timeout:       DefaultTimeout,
		},
		RegistrationServiceNs: registrationServiceNs,
	}
}

// WithRetryOptions returns a new HostAwaitility with the given RetryOptions applied
func (a *HostAwaitility) WithRetryOptions(options ...RetryOption) *HostAwaitility {
	return &HostAwaitility{
		Awaitility:             a.Awaitility.WithRetryOptions(options...),
		RegistrationServiceNs:  a.RegistrationServiceNs,
		RegistrationServiceURL: a.RegistrationServiceURL,
	}
}

func (a *HostAwaitility) sprintAllResources() string {
	all, err := a.allResources()
	buf := &strings.Builder{}
	if err != nil {
		buf.WriteString("unable to list other resources in the host namespace:\n")
		buf.WriteString(err.Error())
		buf.WriteString("\n")
	} else {
		buf.WriteString("other resources in the host namespace:\n")
		for _, r := range all {
			y, _ := yaml.Marshal(r)
			buf.Write(y)
			buf.WriteString("\n")
		}
	}
	return buf.String()
}

// list all relevant resources in the host namespace, in case of test failure and for faster troubleshooting.
func (a *HostAwaitility) allResources() ([]runtime.Object, error) {
	all := []runtime.Object{}
	// usersignups
	usersignups := &toolchainv1alpha1.UserSignupList{}
	if err := a.Client.List(context.TODO(), usersignups, client.InNamespace(a.Namespace)); err != nil {
		return nil, err
	}
	for _, i := range usersignups.Items {
		copy := i
		all = append(all, &copy)
	}
	// masteruserrecords
	masteruserrecords := &toolchainv1alpha1.MasterUserRecordList{}
	if err := a.Client.List(context.TODO(), masteruserrecords, client.InNamespace(a.Namespace)); err != nil {
		return nil, err
	}
	for _, i := range masteruserrecords.Items {
		copy := i
		all = append(all, &copy)
	}
	// notifications
	notifications := &toolchainv1alpha1.NotificationList{}
	if err := a.Client.List(context.TODO(), notifications, client.InNamespace(a.Namespace)); err != nil {
		return nil, err
	}
	for _, i := range notifications.Items {
		copy := i
		all = append(all, &copy)
	}
	// nstemplatetiers
	nstemplatetiers := &toolchainv1alpha1.NSTemplateTierList{}
	if err := a.Client.List(context.TODO(), nstemplatetiers, client.InNamespace(a.Namespace)); err != nil {
		return nil, err
	}
	for _, i := range nstemplatetiers.Items {
		copy := i
		all = append(all, &copy)
	}
	// toolchainconfig
	toolchainconfigs := &toolchainv1alpha1.ToolchainConfigList{}
	if err := a.Client.List(context.TODO(), toolchainconfigs, client.InNamespace(a.Namespace)); err != nil {
		return nil, err
	}
	for _, i := range toolchainconfigs.Items {
		copy := i
		all = append(all, &copy)
	}
	// toolchainstatus
	toolchainstatuses := &toolchainv1alpha1.ToolchainStatusList{}
	if err := a.Client.List(context.TODO(), toolchainstatuses, client.InNamespace(a.Namespace)); err != nil {
		return nil, err
	}
	for _, i := range usersignups.Items {
		copy := i
		all = append(all, &copy)
	}
	return all, nil
}

// WaitForMasterUserRecord waits until there is a MasterUserRecord available with the given name and the optional conditions
func (a *HostAwaitility) WaitForMasterUserRecord(name string, criteria ...MasterUserRecordWaitCriterion) (*toolchainv1alpha1.MasterUserRecord, error) {
	var mur *toolchainv1alpha1.MasterUserRecord
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		mur = obj
		return matchMasterUserRecordWaitCriterion(obj, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printMasterUserRecordWaitCriterionDiffs(mur, criteria...)
	}
	return mur, err
}

func (a *HostAwaitility) GetMasterUserRecord(criteria ...MasterUserRecordWaitCriterion) (*toolchainv1alpha1.MasterUserRecord, error) {
	murList := &toolchainv1alpha1.MasterUserRecordList{}
	if err := a.Client.List(context.TODO(), murList, client.InNamespace(a.Namespace)); err != nil {
		return nil, err
	}
	for _, mur := range murList.Items {
		if matchMasterUserRecordWaitCriterion(&mur, criteria...) {
			return &mur, nil
		}
	}
	// no match found, print the diffs
	a.printMasterUserRecordWaitCriterionDiffs(&toolchainv1alpha1.MasterUserRecord{}, criteria...)
	return nil, nil
}

// UpdateMasterUserRecordSpec tries to update the Spec of the given MasterUserRecord
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and and tries again
// Returns the updated MasterUserRecord
func (a *HostAwaitility) UpdateMasterUserRecordSpec(murName string, modifyMur func(mur *toolchainv1alpha1.MasterUserRecord)) (*toolchainv1alpha1.MasterUserRecord, error) {
	return a.UpdateMasterUserRecord(false, murName, modifyMur)
}

// UpdateMasterUserRecordStatus tries to update the Status of the given MasterUserRecord
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and and tries again
// Returns the updated MasterUserRecord
func (a *HostAwaitility) UpdateMasterUserRecordStatus(murName string, modifyMur func(mur *toolchainv1alpha1.MasterUserRecord)) (*toolchainv1alpha1.MasterUserRecord, error) {
	return a.UpdateMasterUserRecord(true, murName, modifyMur)
}

// UpdateMasterUserRecord tries to update the Spec or the Status of the given MasterUserRecord
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and and tries again
// Returns the updated MasterUserRecord
func (a *HostAwaitility) UpdateMasterUserRecord(status bool, murName string, modifyMur func(mur *toolchainv1alpha1.MasterUserRecord)) (*toolchainv1alpha1.MasterUserRecord, error) {
	var m *toolchainv1alpha1.MasterUserRecord
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		freshMur := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: murName}, freshMur); err != nil {
			return true, err
		}

		modifyMur(freshMur)
		if status {
			// Update status
			if err := a.Client.Status().Update(context.TODO(), freshMur); err != nil {
				a.T.Logf("error updating MasterUserRecord.Status '%s': %s. Will retry again...", murName, err.Error())
				return false, nil
			}
		} else if err := a.Client.Update(context.TODO(), freshMur); err != nil {
			a.T.Logf("error updating MasterUserRecord.Spec '%s': %s. Will retry again...", murName, err.Error())
			return false, nil
		}
		m = freshMur
		return true, nil
	})
	return m, err
}

// UpdateUserSignup tries to update the Spec of the given UserSignup
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and tries again
// Returns the updated UserSignup
func (a *HostAwaitility) UpdateUserSignup(userSignupName string, modifyUserSignup func(us *toolchainv1alpha1.UserSignup)) (*toolchainv1alpha1.UserSignup, error) {
	var userSignup *toolchainv1alpha1.UserSignup
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		freshUserSignup := &toolchainv1alpha1.UserSignup{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: userSignupName}, freshUserSignup); err != nil {
			return true, err
		}

		modifyUserSignup(freshUserSignup)
		if err := a.Client.Update(context.TODO(), freshUserSignup); err != nil {
			a.T.Logf("error updating UserSignup '%s': %s. Will retry again...", userSignupName, err.Error())
			return false, nil
		}
		userSignup = freshUserSignup
		return true, nil
	})
	return userSignup, err
}

// MasterUserRecordWaitCriterion a struct to compare with an expected MasterUserRecord
type MasterUserRecordWaitCriterion struct {
	Match func(*toolchainv1alpha1.MasterUserRecord) bool
	Diff  func(*toolchainv1alpha1.MasterUserRecord) string
}

func matchMasterUserRecordWaitCriterion(actual *toolchainv1alpha1.MasterUserRecord, criteria ...MasterUserRecordWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *HostAwaitility) printMasterUserRecordWaitCriterionDiffs(actual *toolchainv1alpha1.MasterUserRecord, criteria ...MasterUserRecordWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find MasterUserRecord\n")
	} else {
		buf.WriteString("failed to find MasterUserRecord with matching criteria:\n")
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := yaml.Marshal(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	// also include other resources relevant in the host namespace, to help troubleshooting
	buf.WriteString(a.sprintAllResources())

	a.T.Log(buf.String())
}

// UntilMasterUserRecordHasProvisionedTime checks if MasterUserRecord status has the given provisioned time
func UntilMasterUserRecordHasProvisionedTime(expectedTime *v1.Time) MasterUserRecordWaitCriterion {
	return MasterUserRecordWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MasterUserRecord) bool {
			return actual.Status.ProvisionedTime != nil && expectedTime.Time == actual.Status.ProvisionedTime.Time
		},
		Diff: func(actual *toolchainv1alpha1.MasterUserRecord) string {
			if actual.Status.ProvisionedTime == nil {
				return fmt.Sprintf("expected status provisioned time '%s'.\n\tactual is 'nil'", expectedTime.String())
			}
			return fmt.Sprintf("expected status provisioned time '%s'.\n\tactual: '%s'", expectedTime.String(), actual.Status.ProvisionedTime.Time.String())
		},
	}
}

// UntilMasterUserRecordIsBeingDeleted checks if MasterUserRecord has Deletion Timestamp
func UntilMasterUserRecordIsBeingDeleted() MasterUserRecordWaitCriterion {
	return MasterUserRecordWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MasterUserRecord) bool {
			return actual.DeletionTimestamp != nil
		},
	}
}

// UntilMasterUserRecordHasCondition checks if MasterUserRecord status has the given conditions (among others)
func UntilMasterUserRecordHasCondition(expected toolchainv1alpha1.Condition) MasterUserRecordWaitCriterion {
	return MasterUserRecordWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MasterUserRecord) bool {
			return test.ContainsCondition(actual.Status.Conditions, expected)
		},
		Diff: func(actual *toolchainv1alpha1.MasterUserRecord) string {
			e, _ := yaml.Marshal(expected)
			a, _ := yaml.Marshal(actual.Status.Conditions)
			return fmt.Sprintf("expected conditions to contain: %s.\n\tactual: %s", e, a)
		},
	}
}

// UntilMasterUserRecordHasConditions checks if MasterUserRecord status has the given set of conditions
func UntilMasterUserRecordHasConditions(expected ...toolchainv1alpha1.Condition) MasterUserRecordWaitCriterion {
	return MasterUserRecordWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MasterUserRecord) bool {
			return test.ConditionsMatch(actual.Status.Conditions, expected...)
		},
		Diff: func(actual *toolchainv1alpha1.MasterUserRecord) string {
			return fmt.Sprintf("expected conditions to match:\n%s", Diff(expected, actual.Status.Conditions))
		},
	}
}

// UntilMasterUserRecordHasNotSyncIndex checks if MasterUserRecord has a
// sync index *different* from the given value for the given target cluster
func UntilMasterUserRecordHasNotSyncIndex(expected string) MasterUserRecordWaitCriterion {
	return MasterUserRecordWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MasterUserRecord) bool {
			// lookup user account with target cluster
			ua := actual.Spec.UserAccounts[0]
			return ua.SyncIndex != expected
		},
		Diff: func(actual *toolchainv1alpha1.MasterUserRecord) string {
			return fmt.Sprintf("expected sync index to match.\n\twanted: '%s'\n\tactual: '%s'", expected, actual.Spec.UserAccounts[0].SyncIndex)
		},
	}
}

func WithMurName(name string) MasterUserRecordWaitCriterion {
	return MasterUserRecordWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MasterUserRecord) bool {
			return actual.Name == name
		},
		Diff: func(actual *toolchainv1alpha1.MasterUserRecord) string {
			return fmt.Sprintf("expected MasterUserRecord named '%s'", name)
		},
	}
}

// UntilMasterUserRecordHasUserAccountStatuses checks if MasterUserRecord status has the given set of status embedded UserAccounts
func UntilMasterUserRecordHasUserAccountStatuses(expected ...toolchainv1alpha1.UserAccountStatusEmbedded) MasterUserRecordWaitCriterion {
	return MasterUserRecordWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MasterUserRecord) bool {
			if len(actual.Status.UserAccounts) != len(expected) {
				return false
			}
			for _, expUaStatus := range expected {
				expUaStatus.SyncIndex = getUaSpecSyncIndex(actual, expUaStatus.Cluster.Name)
				if !containsUserAccountStatus(actual.Status.UserAccounts, expUaStatus) {
					return false
				}
			}
			return true
		},
		Diff: func(actual *toolchainv1alpha1.MasterUserRecord) string {
			return fmt.Sprintf("expected UserAccount statuses to match: %s", Diff(expected, actual.Status.UserAccounts))
		},
	}
}

// UserSignupWaitCriterion a struct to compare with an expected UserSignup
type UserSignupWaitCriterion struct {
	Match func(*toolchainv1alpha1.UserSignup) bool
	Diff  func(*toolchainv1alpha1.UserSignup) string
}

func matchUserSignupWaitCriterion(actual *toolchainv1alpha1.UserSignup, criteria ...UserSignupWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *HostAwaitility) printUserSignupWaitCriterionDiffs(actual *toolchainv1alpha1.UserSignup, criteria ...UserSignupWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find UserSignup\n")
	} else {
		buf.WriteString("failed to find UserSignup with matching criteria:\n")
		buf.WriteString("actual:\n")
		y, _ := yaml.Marshal(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	// also include other resources relevant in the host namespace, to help troubleshooting
	buf.WriteString(a.sprintAllResources())

	a.T.Log(buf.String())
}

// UntilUserSignupIsBeingDeleted returns a `UserSignupWaitCriterion` which checks that the given
// UserSignup has deletion timestamp set
func UntilUserSignupIsBeingDeleted() UserSignupWaitCriterion {
	return UserSignupWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserSignup) bool {
			return actual.DeletionTimestamp != nil
		},
		Diff: func(_ *toolchainv1alpha1.UserSignup) string {
			return "expected a non-nil DeletionTimestamp"
		},
	}
}

// UntilUserSignupHasConditions returns a `UserAccountWaitCriterion` which checks that the given
// UserAccount has exactly all the given status conditions
func UntilUserSignupHasConditions(expected ...toolchainv1alpha1.Condition) UserSignupWaitCriterion {
	return UserSignupWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserSignup) bool {
			return test.ConditionsMatch(actual.Status.Conditions, expected...)
		},
		Diff: func(actual *toolchainv1alpha1.UserSignup) string {
			return fmt.Sprintf("expected conditions to match:\n%s", Diff(expected, actual.Status.Conditions))
		},
	}
}

// ContainsCondition returns a `UserAccountWaitCriterion` which checks that the given
// UserAccount contains the given status condition
func ContainsCondition(expected toolchainv1alpha1.Condition) UserSignupWaitCriterion {
	return UserSignupWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserSignup) bool {
			return test.ContainsCondition(actual.Status.Conditions, expected)
		},
		Diff: func(actual *toolchainv1alpha1.UserSignup) string {
			e, _ := yaml.Marshal(expected)
			a, _ := yaml.Marshal(actual.Status.Conditions)
			return fmt.Sprintf("expected conditions to contain: %s.\n\tactual: %s", e, a)
		},
	}
}

// UntilUserSignupHasStateLabel returns a `UserAccountWaitCriterion` which checks that the given
// UserAccount has toolchain.dev.openshift.com/state equal to the given value
func UntilUserSignupHasStateLabel(expected string) UserSignupWaitCriterion {
	return UserSignupWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserSignup) bool {
			return actual.Labels != nil && actual.Labels[toolchainv1alpha1.UserSignupStateLabelKey] == expected
		},
		Diff: func(actual *toolchainv1alpha1.UserSignup) string {
			if len(actual.Labels) == 0 {
				return fmt.Sprintf("expected to have a label with key '%s' (and value", toolchainv1alpha1.UserSignupStateLabelKey)
			}
			return fmt.Sprintf("expected value of label '%s' to equal '%s'. Actual: '%s'", toolchainv1alpha1.UserSignupStateLabelKey, expected, actual.Labels[toolchainv1alpha1.UserSignupStateLabelKey])
		},
	}
}

// WaitForTestResourcesCleanup waits for all UserSignup and MasterUserRecord deletions to complete
func (a *HostAwaitility) WaitForTestResourcesCleanup(initialDelay time.Duration) error {
	a.T.Logf("waiting for resource cleanup")
	time.Sleep(initialDelay)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		usList := &toolchainv1alpha1.UserSignupList{}
		if err := a.Client.List(context.TODO(), usList, client.InNamespace(a.Namespace)); err != nil {
			return false, err
		}
		for _, us := range usList.Items {
			if us.DeletionTimestamp != nil {
				return false, nil
			}
		}

		murList := &toolchainv1alpha1.MasterUserRecordList{}
		if err := a.Client.List(context.TODO(), murList, client.InNamespace(a.Namespace)); err != nil {
			return false, err
		}
		for _, mur := range murList.Items {
			if mur.DeletionTimestamp != nil {
				return false, nil
			}
		}
		return true, nil
	})
}

// WaitForUserSignup waits until there is a UserSignup available with the given name and set of status conditions
func (a *HostAwaitility) WaitForUserSignup(name string, criteria ...UserSignupWaitCriterion) (*toolchainv1alpha1.UserSignup, error) {
	a.T.Logf("waiting for UserSignup '%s' in namespace '%s' to match criteria", name, a.Namespace)
	var userSignup *toolchainv1alpha1.UserSignup
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.UserSignup{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		userSignup = obj
		return matchUserSignupWaitCriterion(userSignup, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printUserSignupWaitCriterionDiffs(userSignup, criteria...)
	}
	return userSignup, err
}

// WaitForBannedUser waits until there is a BannedUser available with the given email
func (a *HostAwaitility) WaitForBannedUser(email string) (*toolchainv1alpha1.BannedUser, error) {
	a.T.Logf("waiting for BannedUser for user '%s' in namespace '%s'", email, a.Namespace)
	var bannedUser *toolchainv1alpha1.BannedUser
	labels := map[string]string{toolchainv1alpha1.BannedUserEmailHashLabelKey: md5.CalcMd5(email)}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		bannedUserList := &toolchainv1alpha1.BannedUserList{}
		if err = a.Client.List(context.TODO(), bannedUserList, client.MatchingLabels(labels), client.InNamespace(a.Namespace)); err != nil {
			if len(bannedUserList.Items) == 0 {
				return false, nil
			}
			return false, err
		}
		bannedUser = &bannedUserList.Items[0]
		return true, nil
	})
	// log message if an error occurred
	if err != nil {
		a.T.Logf("failed to find Banned for email address '%s': %v", email, err)
	}
	return bannedUser, err
}

// DeleteToolchainStatus deletes the ToolchainStatus resource with the given name and in the host operator namespace
func (a *HostAwaitility) DeleteToolchainStatus(name string) error {
	a.T.Logf("deleting ToolchainStatus '%s' in namespace '%s'", name, a.Namespace)
	toolchainstatus := &toolchainv1alpha1.ToolchainStatus{}
	if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, toolchainstatus); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return a.Client.Delete(context.TODO(), toolchainstatus)
}

// WaitUntilBannedUserDeleted waits until the BannedUser with the given name is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilBannedUserDeleted(name string) error {
	a.T.Logf("waiting until BannedUser '%s' in namespace '%s' is deleted", name, a.Namespace)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		mur := &toolchainv1alpha1.BannedUser{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, mur); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

// WaitUntilUserSignupDeleted waits until the UserSignup with the given name is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilUserSignupDeleted(name string) error {
	a.T.Logf("waiting until UserSignup '%s' in namespace '%s is deleted", name, a.Namespace)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		userSignup := &toolchainv1alpha1.UserSignup{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, userSignup); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

// WaitUntilMasterUserRecordDeleted waits until the MUR with the given name is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilMasterUserRecordDeleted(name string) error {
	a.T.Logf("waiting until MasterUserRecord '%s' in namespace '%s' is deleted", name, a.Namespace)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		mur := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, mur); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

// CheckMasterUserRecordIsDeleted checks that the MUR with the given name is not present and won't be created in the next 2 seconds
func (a *HostAwaitility) CheckMasterUserRecordIsDeleted(name string) {
	a.T.Logf("checking that MasterUserRecord '%s' in namespace '%s' is deleted", name, a.Namespace)
	err := wait.Poll(a.RetryInterval, 2*time.Second, func() (done bool, err error) {
		mur := &toolchainv1alpha1.MasterUserRecord{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, mur); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return false, fmt.Errorf("the MasterUserRecord '%s' should not be present, but it is", name)
	})
	require.Equal(a.T, wait.ErrWaitTimeout, err)
}

func getUaSpecSyncIndex(mur *toolchainv1alpha1.MasterUserRecord, cluster string) string {
	for _, ua := range mur.Spec.UserAccounts {
		if ua.TargetCluster == cluster {
			return ua.SyncIndex
		}
	}
	return ""
}

func containsUserAccountStatus(uaStatuses []toolchainv1alpha1.UserAccountStatusEmbedded, uaStatus toolchainv1alpha1.UserAccountStatusEmbedded) bool {
	for _, status := range uaStatuses {
		if reflect.DeepEqual(uaStatus.Cluster, status.Cluster) &&
			uaStatus.SyncIndex == status.SyncIndex &&
			test.ConditionsMatch(uaStatus.Conditions, status.Conditions...) {
			return true
		}
	}
	return false
}

// WaitForNSTemplateTier waits until an NSTemplateTier with the given name exists and matches the given conditions
func (a *HostAwaitility) WaitForNSTemplateTier(name string, criteria ...NSTemplateTierWaitCriterion) (*toolchainv1alpha1.NSTemplateTier, error) {
	a.T.Logf("waiting until NSTemplateTier '%s' in namespace '%s' matches criteria", name, a.Namespace)
	tier := &toolchainv1alpha1.NSTemplateTier{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.NSTemplateTier{}
		err = a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj)
		if err != nil && !errors.IsNotFound(err) {
			// return the error
			return false, err
		} else if errors.IsNotFound(err) {
			// keep waiting
			return false, nil
		}
		tier = obj
		return matchNSTemplateTierWaitCriterion(obj, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printNSTemplateTierWaitCriterionDiffs(tier, criteria...)
	}
	require.NoError(a.T, err)

	// now, check that the `templateRef` field is set for each namespace and clusterResources (if applicable)
	// and that there's a TierTemplate resource with the same name
	for i, ns := range tier.Spec.Namespaces {
		if ns.TemplateRef == "" {
			return nil, fmt.Errorf("missing 'templateRef' in namespace #%d in NSTemplateTier '%s'", i, tier.Name)
		}
		if _, err := a.WaitForTierTemplate(ns.TemplateRef); err != nil {
			return nil, err
		}
	}
	if tier.Spec.ClusterResources != nil {
		if tier.Spec.ClusterResources.TemplateRef == "" {
			return nil, fmt.Errorf("missing 'templateRef' for the cluster resources in NSTemplateTier '%s'", tier.Name)
		}
		if _, err := a.WaitForTierTemplate(tier.Spec.ClusterResources.TemplateRef); err != nil {
			return nil, err
		}
	}
	return tier, err
}

// WaitForTierTemplate waits until a TierTemplate with the given name exists
// Returns an error if the resource did not exist (or something wrong happened)
func (a *HostAwaitility) WaitForTierTemplate(name string) (*toolchainv1alpha1.TierTemplate, error) { // nolint: unparam
	tierTemplate := &toolchainv1alpha1.TierTemplate{}
	a.T.Logf("waiting until TierTemplate '%s' exists in namespace '%s'...", name, a.Namespace)
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.TierTemplate{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		tierTemplate = obj
		return true, nil
	})
	// log message if an error occurred
	if err != nil {
		a.T.Logf("failed to find TierTemplate '%s': %v", name, err)
	}
	return tierTemplate, err
}

// NSTemplateTierWaitCriterion a struct to compare with an expected NSTemplateTier
type NSTemplateTierWaitCriterion struct {
	Match func(*toolchainv1alpha1.NSTemplateTier) bool
	Diff  func(*toolchainv1alpha1.NSTemplateTier) string
}

func matchNSTemplateTierWaitCriterion(actual *toolchainv1alpha1.NSTemplateTier, criteria ...NSTemplateTierWaitCriterion) bool {
	for _, c := range criteria {
		// if at least one criteria does not match, keep waiting
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *HostAwaitility) printNSTemplateTierWaitCriterionDiffs(actual *toolchainv1alpha1.NSTemplateTier, criteria ...NSTemplateTierWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find NSTemplateTier\n")
	} else {
		buf.WriteString("failed to find NSTemplateTier with matching criteria:\n")
		buf.WriteString("actual:\n")
		y, _ := yaml.Marshal(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	// also include other resources relevant in the host namespace, to help troubleshooting
	buf.WriteString(a.sprintAllResources())

	a.T.Log(buf.String())
}

// NSTemplateTierSpecMatcher a struct to compare with an expected NSTemplateTierSpec
type NSTemplateTierSpecMatcher struct {
	Match func(toolchainv1alpha1.NSTemplateTierSpec) bool
	Diff  func(toolchainv1alpha1.NSTemplateTierSpec) string
}

// UntilNSTemplateTierSpec verify that the NSTemplateTier spec has the specified condition
func UntilNSTemplateTierSpec(matcher NSTemplateTierSpecMatcher) NSTemplateTierWaitCriterion {
	return NSTemplateTierWaitCriterion{
		Match: func(actual *toolchainv1alpha1.NSTemplateTier) bool {
			return matcher.Match(actual.Spec)
		},
		Diff: func(actual *toolchainv1alpha1.NSTemplateTier) string {
			return matcher.Diff(actual.Spec)
		},
	}
}

// UntilNSTemplateTierStatusUpdates verify that the NSTemplateTier status.Updates has the specified number of entries
func UntilNSTemplateTierStatusUpdates(expected int) NSTemplateTierWaitCriterion {
	return NSTemplateTierWaitCriterion{
		Match: func(actual *toolchainv1alpha1.NSTemplateTier) bool {
			return len(actual.Status.Updates) == expected
		},
		Diff: func(actual *toolchainv1alpha1.NSTemplateTier) string {
			return fmt.Sprintf("expected status.updates count %d. Actual: %d", expected, len(actual.Status.Updates))
		},
	}
}

// HasNoTemplateRefWithSuffix checks that ALL namespaces' `TemplateRef` doesn't have the suffix
func HasNoTemplateRefWithSuffix(suffix string) NSTemplateTierSpecMatcher {
	return NSTemplateTierSpecMatcher{
		Match: func(actual toolchainv1alpha1.NSTemplateTierSpec) bool {
			for _, ns := range actual.Namespaces {
				if strings.HasSuffix(ns.TemplateRef, suffix) {
					return false
				}
			}
			return !strings.HasSuffix(actual.ClusterResources.TemplateRef, suffix)
		},
		Diff: func(actual toolchainv1alpha1.NSTemplateTierSpec) string {
			a, _ := yaml.Marshal(actual)
			return fmt.Sprintf("expected no TemplateRef with suffix '%s'. Actual: %s", suffix, a)
		},
	}
}

// HasClusterResourcesTemplateRef checks that the clusterResources `TemplateRef` match the given value
func HasClusterResourcesTemplateRef(expected string) NSTemplateTierSpecMatcher {
	return NSTemplateTierSpecMatcher{
		Match: func(actual toolchainv1alpha1.NSTemplateTierSpec) bool {
			return actual.ClusterResources.TemplateRef == expected
		},
		Diff: func(actual toolchainv1alpha1.NSTemplateTierSpec) string {
			return fmt.Sprintf("expected no ClusterResources.TemplateRef to equal '%s'. Actual: '%s'", expected, actual.ClusterResources.TemplateRef)
		},
	}
}

// WaitForChangeTierRequest waits until there a ChangeTierRequest is available with the given status conditions
func (a *HostAwaitility) WaitForChangeTierRequest(name string, expected toolchainv1alpha1.Condition) (*toolchainv1alpha1.ChangeTierRequest, error) {
	var changeTierRequest *toolchainv1alpha1.ChangeTierRequest
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.ChangeTierRequest{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		changeTierRequest = obj
		return test.ConditionsMatch(obj.Status.Conditions, expected), nil
	})
	// log message if an error occurred
	if err != nil {
		if changeTierRequest == nil {
			e, _ := yaml.Marshal(expected)
			a.T.Logf("failed to find ChangeTierRequest '%s' with condition\n%s. Actual: nil", name, e)
		} else {
			a.T.Logf("expected conditions to match: '%s'", Diff(expected, changeTierRequest.Status.Conditions))
		}
	}
	return changeTierRequest, err
}

// WaitUntilChangeTierRequestDeleted waits until the ChangeTierRequest with the given name is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilChangeTierRequestDeleted(name string) error {
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		changeTierRequest := &toolchainv1alpha1.ChangeTierRequest{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, changeTierRequest); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
	// log message if an error occurred
	if err != nil {
		a.T.Logf("failed to wait until ChangeTierRequest '%s' was deleted: %v\n", name, err)
	}
	return err
}

// WaitForTemplateUpdateRequests waits until there is exactly `count` number of TemplateUpdateRequests
func (a *HostAwaitility) WaitForTemplateUpdateRequests(namespace string, count int) error {
	templateUpdateRequests := &toolchainv1alpha1.TemplateUpdateRequestList{}
	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		templateUpdateRequests = &toolchainv1alpha1.TemplateUpdateRequestList{}
		if err := a.Client.List(context.TODO(), templateUpdateRequests, client.InNamespace(namespace)); err != nil {
			return false, err
		}
		return len(templateUpdateRequests.Items) == count, nil
	})
	// log message if an error occurred
	if err != nil {
		requests, _ := yaml.Marshal(templateUpdateRequests)

		a.T.Logf("the actual number '%d' of TemplateUpdateRequests in namespace '%s' doesn't match the expected one '%d'.",
			len(templateUpdateRequests.Items), namespace, count)
		a.T.Logf("TemplateUpdateRequests present in the namespace:\n%s", requests)
		a.listAndPrint("MasterUserRecords", namespace, &toolchainv1alpha1.MasterUserRecordList{})
		a.listAndPrint("NSTemplateTiers", namespace, &toolchainv1alpha1.NSTemplateTierList{})
	}
	return err
}

// NotificationWaitCriterion a struct to compare with an expected Notification
type NotificationWaitCriterion struct {
	Match func(toolchainv1alpha1.Notification) bool
	Diff  func(toolchainv1alpha1.Notification) string
}

func matchNotificationWaitCriterion(actual []toolchainv1alpha1.Notification, criteria ...NotificationWaitCriterion) bool {
	for _, n := range actual {
		for _, c := range criteria {
			if !c.Match(n) {
				return false
			}
		}
	}
	return true
}

func (a *HostAwaitility) printNotificationWaitCriterionDiffs(actual []toolchainv1alpha1.Notification, criteria ...NotificationWaitCriterion) {
	buf := &strings.Builder{}
	if len(actual) == 0 {
		buf.WriteString("no notification found\n")
	} else {
		buf.WriteString("failed to find notifications with matching criteria:\n")
		buf.WriteString("actual:\n")
		y, _ := yaml.Marshal(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, n := range actual {
			for _, c := range criteria {
				if !c.Match(n) {
					buf.WriteString(c.Diff(n))
					buf.WriteString("\n")
				}
			}
		}
	}
	// also include other resources relevant in the host namespace, to help troubleshooting
	buf.WriteString(a.sprintAllResources())

	a.T.Log(buf.String())
}

// WaitForNotifications waits until there is an expected number of Notifications available for the provided user and with the notification type and which match the conditions (if provided).
func (a *HostAwaitility) WaitForNotifications(username, notificationType string, numberOfNotifications int, criteria ...NotificationWaitCriterion) ([]toolchainv1alpha1.Notification, error) {
	a.T.Logf("waiting for notifications to match criteria for user '%s'", username)
	var notifications []toolchainv1alpha1.Notification
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		labels := map[string]string{toolchainv1alpha1.NotificationUserNameLabelKey: username, toolchainv1alpha1.NotificationTypeLabelKey: notificationType}
		opts := client.MatchingLabels(labels)
		notificationList := &toolchainv1alpha1.NotificationList{}
		if err := a.Client.List(context.TODO(), notificationList, opts); err != nil {
			return false, err
		}
		notifications = notificationList.Items
		if numberOfNotifications != len(notificationList.Items) {
			return false, nil
		}
		return matchNotificationWaitCriterion(notificationList.Items, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printNotificationWaitCriterionDiffs(notifications, criteria...)
	}
	return notifications, err
}

// WaitUntilNotificationsDeleted waits until the Notification for the given user is deleted (ie, not found)
func (a *HostAwaitility) WaitUntilNotificationsDeleted(username, notificationType string) error {
	a.T.Logf("waiting until notifications have been deleted for user '%s'", username)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		labels := map[string]string{toolchainv1alpha1.NotificationUserNameLabelKey: username, toolchainv1alpha1.NotificationTypeLabelKey: notificationType}
		opts := client.MatchingLabels(labels)
		notificationList := &toolchainv1alpha1.NotificationList{}
		if err := a.Client.List(context.TODO(), notificationList, opts); err != nil {
			return false, err
		}
		return len(notificationList.Items) == 0, nil
	})
}

// UntilNotificationHasConditions checks if Notification status has the given set of conditions
func UntilNotificationHasConditions(expected ...toolchainv1alpha1.Condition) NotificationWaitCriterion {
	return NotificationWaitCriterion{
		Match: func(actual toolchainv1alpha1.Notification) bool {
			return test.ConditionsMatch(actual.Status.Conditions, expected...)
		},
		Diff: func(actual toolchainv1alpha1.Notification) string {
			return fmt.Sprintf("expected Notification conditions to match:\n%s", Diff(expected, actual.Status.Conditions))
		},
	}
}

// ToolchainStatusWaitCriterion a struct to compare with an expected ToolchainStatus
type ToolchainStatusWaitCriterion struct {
	Match func(*toolchainv1alpha1.ToolchainStatus) bool
	Diff  func(*toolchainv1alpha1.ToolchainStatus) string
}

func matchToolchainStatusWaitCriterion(actual *toolchainv1alpha1.ToolchainStatus, criteria ...ToolchainStatusWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *HostAwaitility) printToolchainStatusWaitCriterionDiffs(actual *toolchainv1alpha1.ToolchainStatus, criteria ...ToolchainStatusWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find Toolchainstatus\n")
	} else {
		buf.WriteString("failed to find ToolchainStatus with matching criteria:\n")
		buf.WriteString("actual:\n")
		y, _ := yaml.Marshal(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	// also include other resources relevant in the host namespace, to help troubleshooting
	buf.WriteString(a.sprintAllResources())

	a.T.Log(buf.String())
}

// UntilToolchainStatusHasConditions returns a `ToolchainStatusWaitCriterion` which checks that the given
// ToolchainStatus has exactly all the given status conditions
func UntilToolchainStatusHasConditions(expected ...toolchainv1alpha1.Condition) ToolchainStatusWaitCriterion {
	return ToolchainStatusWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainStatus) bool {
			return test.ConditionsMatch(actual.Status.Conditions, expected...)
		},
		Diff: func(actual *toolchainv1alpha1.ToolchainStatus) string {
			return fmt.Sprintf("expected ToolchainStatus conditions to match:\n%s", Diff(expected, actual.Status.Conditions))
		},
	}
}

// UntilAllMembersHaveUsageSet returns a `ToolchainStatusWaitCriterion` which checks that the given
// ToolchainStatus has all members with some non-zero resource usage
func UntilAllMembersHaveUsageSet() ToolchainStatusWaitCriterion {
	return ToolchainStatusWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainStatus) bool {
			for _, member := range actual.Status.Members {
				if !hasMemberStatusUsageSet(member.MemberStatus) {
					return false
				}
			}
			return true
		},
		Diff: func(actual *toolchainv1alpha1.ToolchainStatus) string {
			a, _ := yaml.Marshal(actual.Status.Members)
			return fmt.Sprintf("expected all status members to have usage set. Actual: %s", a)
		},
	}
}

func UntilAllMembersHaveAPIEndpoint(apiEndpoint string) ToolchainStatusWaitCriterion {
	return ToolchainStatusWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainStatus) bool {
			//Since all member operators currently run in the same cluster in the e2e test environment, then using the same memberCluster.Spec.APIEndpoint for all the member clusters should be fine.
			for _, member := range actual.Status.Members {
				// check Member field ApiEndpoint is assigned
				if member.ApiEndpoint != apiEndpoint {
					return false
				}
			}
			return true
		},
		Diff: func(actual *toolchainv1alpha1.ToolchainStatus) string {
			a, _ := yaml.Marshal(actual.Status.Members)
			return fmt.Sprintf("expected all status members to have API Endpoint '%s'. Actual: %s", apiEndpoint, a)
		},
	}
}

// UntilHasMurCount returns a `ToolchainStatusWaitCriterion` which checks that the given
// ToolchainStatus has the given count of MasterUserRecords
func UntilHasMurCount(domain string, expectedCount int) ToolchainStatusWaitCriterion {
	return ToolchainStatusWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainStatus) bool {
			murs, ok := actual.Status.Metrics[toolchainv1alpha1.MasterUserRecordsPerDomainMetricKey]
			if !ok {
				return false
			}
			return murs[domain] == expectedCount
		},
		Diff: func(actual *toolchainv1alpha1.ToolchainStatus) string {
			murs, ok := actual.Status.Metrics[toolchainv1alpha1.MasterUserRecordsPerDomainMetricKey]
			if !ok {
				return "MasterUserRecordPerDomain metric not found"
			}
			return fmt.Sprintf("expected MasterUserRecordPerDomain metric to be %d. Actual: %d", expectedCount, murs[domain])
		},
	}
}

// WaitForToolchainStatus waits until the ToolchainStatus is available with the provided criteria, if any
func (a *HostAwaitility) WaitForToolchainStatus(criteria ...ToolchainStatusWaitCriterion) (*toolchainv1alpha1.ToolchainStatus, error) {
	// there should only be one toolchain status with the name toolchain-status
	name := "toolchain-status"
	toolchainStatus := &toolchainv1alpha1.ToolchainStatus{}
	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.ToolchainStatus{}
		// retrieve the toolchainstatus from the host namespace
		err = a.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: a.Namespace,
				Name:      name,
			},
			obj)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		toolchainStatus = obj
		return matchToolchainStatusWaitCriterion(toolchainStatus, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printToolchainStatusWaitCriterionDiffs(toolchainStatus, criteria...)
	}
	return toolchainStatus, err
}

// GetToolchainConfig returns ToolchainConfig instance, nil if not found
func (a *HostAwaitility) GetToolchainConfig() *toolchainv1alpha1.ToolchainConfig {
	config := &toolchainv1alpha1.ToolchainConfig{}
	if err := a.Client.Get(context.TODO(), test.NamespacedName(a.Namespace, "config"), config); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		require.NoError(a.T, err)
	}
	return config
}

// ToolchainConfigWaitCriterion a struct to compare with an expected ToolchainConfig
type ToolchainConfigWaitCriterion struct {
	Match func(*toolchainv1alpha1.ToolchainConfig) bool
	Diff  func(*toolchainv1alpha1.ToolchainConfig) string
}

func matchToolchainConfigWaitCriterion(actual *toolchainv1alpha1.ToolchainConfig, criteria ...ToolchainConfigWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *HostAwaitility) printToolchainConfigWaitCriterionDiffs(actual *toolchainv1alpha1.ToolchainConfig, criteria ...ToolchainConfigWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find ToolchainConfig\n")
	} else {
		buf.WriteString("failed to find ToolchainConfig with matching criteria:\n")
		buf.WriteString("actual:\n")
		y, _ := yaml.Marshal(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	// also include other resources relevant in the host namespace, to help troubleshooting
	buf.WriteString(a.sprintAllResources())

	a.T.Log(buf.String())
}

func UntilToolchainConfigHasSyncedStatus(expected toolchainv1alpha1.Condition) ToolchainConfigWaitCriterion {
	return ToolchainConfigWaitCriterion{
		Match: func(actual *toolchainv1alpha1.ToolchainConfig) bool {
			return test.ContainsCondition(actual.Status.Conditions, expected)
		},
		Diff: func(actual *toolchainv1alpha1.ToolchainConfig) string {
			e, _ := yaml.Marshal(expected)
			a, _ := yaml.Marshal(actual.Status.Conditions)
			return fmt.Sprintf("expected conditions to contain: %s.\n\tactual: %s", e, a)
		},
	}
}

// WaitForToolchainConfig waits until the ToolchainConfig is available with the provided criteria, if any
func (a *HostAwaitility) WaitForToolchainConfig(criteria ...ToolchainConfigWaitCriterion) (*toolchainv1alpha1.ToolchainConfig, error) {
	// there should only be one ToolchainConfig with the name "config"
	name := "config"
	var toolchainConfig *toolchainv1alpha1.ToolchainConfig
	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.ToolchainConfig{}
		// retrieve the ToolchainConfig from the host namespace
		if err := a.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: a.Namespace,
				Name:      name,
			},
			obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		toolchainConfig = obj
		return matchToolchainConfigWaitCriterion(toolchainConfig, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printToolchainConfigWaitCriterionDiffs(toolchainConfig, criteria...)
	}
	return toolchainConfig, err
}

// UpdateToolchainConfig updates the current resource of the ToolchainConfig CR with the given options.
// If there is no existing resource already, then it creates a new one.
// At the end of the test it returns the resource back to the original value/state.
func (a *HostAwaitility) UpdateToolchainConfig(options ...testconfig.ToolchainConfigOption) {
	var originalConfig *toolchainv1alpha1.ToolchainConfig
	// try to get the current ToolchainConfig
	config := a.GetToolchainConfig()
	if config == nil {
		// if it doesn't exist, then create a new one
		config = &toolchainv1alpha1.ToolchainConfig{
			ObjectMeta: v1.ObjectMeta{
				Namespace: a.Namespace,
				Name:      "config",
			},
		}
	} else {
		// if it exists then create a copy to store the original values
		originalConfig = config.DeepCopy()
	}

	// modify using the given options
	for _, option := range options {
		option.Apply(config)
	}

	// if it didn't exist before
	if originalConfig == nil {
		// then create a new one
		err := a.Client.Create(context.TODO(), config)
		require.NoError(a.T, err)

		// and as a cleanup function delete it at the end of the test
		a.T.Cleanup(func() {
			err := a.Client.Delete(context.TODO(), config)
			if err != nil && !errors.IsNotFound(err) {
				require.NoError(a.T, err)
			}
		})
		return
	}

	// if the config did exist before the tests, then update it
	err := a.updateToolchainConfigWithRetry(config)
	require.NoError(a.T, err)

	// and as a cleanup function update it back to the original value
	a.T.Cleanup(func() {
		config := a.GetToolchainConfig()
		// if the current config wasn't found
		if config == nil {
			if originalConfig != nil {
				// then create it back with the original values
				err := a.Client.Create(context.TODO(), originalConfig)
				require.NoError(a.T, err)
			}
		} else {
			// otherwise just update it
			err := a.updateToolchainConfigWithRetry(originalConfig)
			require.NoError(a.T, err)
		}
	})
}

// updateToolchainConfigWithRetry attempts to update the toolchainconfig, helpful because the toolchainconfig controller updates the toolchainconfig
// resource periodically which can cause errors like `Operation cannot be fulfilled on toolchainconfigs.toolchain.dev.openshift.com "config": the object has been modified; please apply your changes to the latest version and try again`
// in some cases. Retrying mitigates the potential for test flakiness due to this behaviour.
func (a *HostAwaitility) updateToolchainConfigWithRetry(updatedConfig *toolchainv1alpha1.ToolchainConfig) error {
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		config := a.GetToolchainConfig()
		config.Spec = updatedConfig.Spec
		if err := a.Client.Update(context.TODO(), config); err != nil {
			a.T.Logf("Retrying ToolchainConfig update due to error: %s", err.Error())
			return false, nil
		}
		return true, nil
	})
	return err
}

// GetHostOperatorPod returns the pod running the host operator controllers
func (a *HostAwaitility) GetHostOperatorPod() (corev1.Pod, error) {
	pods := corev1.PodList{}
	if err := a.Client.List(context.TODO(), &pods, client.InNamespace(a.Namespace), client.MatchingLabels{"control-plane": "controller-manager"}); err != nil {
		return corev1.Pod{}, err
	}
	if len(pods.Items) != 1 {
		return corev1.Pod{}, fmt.Errorf("unexpected number of pods with label 'control-plane=controller-manager' in namespace '%s': %d ", a.Namespace, len(pods.Items))
	}
	return pods.Items[0], nil
}
