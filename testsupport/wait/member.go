package wait

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	appstudiov1 "github.com/codeready-toolchain/toolchain-e2e/testsupport/appstudio/api/v1alpha1"
	"github.com/davecgh/go-spew/spew"
	"github.com/ghodss/yaml"
	quotav1 "github.com/openshift/api/quota/v1"
	routev1 "github.com/openshift/api/route/v1"
	userv1 "github.com/openshift/api/user/v1"
	"github.com/redhat-cop/operator-utils/pkg/util"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	netv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	schedulingv1 "k8s.io/api/scheduling/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	appMemberOperatorWebhookLabel = map[string]string{
		"app": "member-operator-webhook",
	}
	codereadyToolchainProviderLabel = map[string]string{
		"toolchain.dev.openshift.com/provider": "codeready-toolchain",
	}
	bothWebhookLabels = map[string]string{
		"app":                                  "member-operator-webhook",
		"toolchain.dev.openshift.com/provider": "codeready-toolchain",
	}
)

type MemberAwaitility struct {
	*Awaitility
}

func NewMemberAwaitility(cfg *rest.Config, cl client.Client, ns, clusterName string) *MemberAwaitility {
	return &MemberAwaitility{
		Awaitility: &Awaitility{
			Client:        cl,
			RestConfig:    cfg,
			ClusterName:   clusterName,
			Namespace:     ns,
			Type:          cluster.Member,
			RetryInterval: DefaultRetryInterval,
			Timeout:       DefaultTimeout,
		},
	}
}

// WaitForMetricsService verifies that there is a service called `host-operator-metrics-service`
// in the member namespace.
func (a *MemberAwaitility) WaitForMetricsService(t *testing.T) {
	_, err := a.WaitForService(t, "member-operator-metrics-service")
	require.NoError(t, err, "failed while waiting for 'member-operator-metrics-service' service")
}

// metric constants
const (
	MemberOperatorVersionMetric = "sandbox_member_operator_version"
)

// InitMetricsAssertion waits for any pending usersignups and then initialized the metrics assertion helper with baseline values
func (a *MemberAwaitility) InitMetrics(t *testing.T) {
	a.WaitForMetricsService(t)
	// Capture baseline values
	a.baselineValues = make(map[string]float64)
	a.baselineValues[MemberOperatorVersionMetric] = a.GetMetricValue(t, MemberOperatorVersionMetric)
	t.Logf("captured baselines:\n%s", spew.Sdump(a.baselineValues))
}

func (a *MemberAwaitility) WithRetryOptions(options ...RetryOption) *MemberAwaitility {
	return &MemberAwaitility{
		Awaitility: a.Awaitility.WithRetryOptions(options...),
	}
}

// UserAccountWaitCriterion a struct to compare with a given UserAccount
type UserAccountWaitCriterion struct {
	Match func(*toolchainv1alpha1.UserAccount) bool
	Diff  func(*toolchainv1alpha1.UserAccount) string
}

func matchUserAccountWaitCriterion(actual *toolchainv1alpha1.UserAccount, criteria ...UserAccountWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *MemberAwaitility) printUserAccountWaitCriterionDiffs(t *testing.T, actual *toolchainv1alpha1.UserAccount, criteria ...UserAccountWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find UserAccount\n")
		buf.WriteString(a.listAndReturnContent("UserAccount", a.Namespace, &toolchainv1alpha1.UserAccountList{}))
	} else {
		buf.WriteString("failed to find UserAccount with matching criteria:\n")
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) && c.Diff != nil {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	t.Log(buf.String())
}

// UntilUserAccountHasLabelWithValue returns a `UserAccountWaitCriterion` which checks that the given
// UserAccount has the expected label with the given value
func UntilUserAccountHasLabelWithValue(key, value string) UserAccountWaitCriterion {
	return UserAccountWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserAccount) bool {
			return actual.Labels[key] == value
		},
		Diff: func(actual *toolchainv1alpha1.UserAccount) string {
			return fmt.Sprintf("expected useraccount to contain label %s:%s:\n%s", key, value, spew.Sdump(actual.Labels))
		},
	}
}

// UntilUserAccountHasAnnotation checks if the UserAccount has the expected annotation
func UntilUserAccountHasAnnotation(key, value string) UserAccountWaitCriterion {
	return UserAccountWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserAccount) bool {
			actualValue, exist := actual.Annotations[key]
			return exist && actualValue == value
		},
		Diff: func(actual *toolchainv1alpha1.UserAccount) string {
			return fmt.Sprintf("expected UserAccount annotation '%s' to be '%s'\nbut it was '%s'", key, value, actual.Annotations[key])
		},
	}
}

// UntilUserAccountHasSpec returns a `UserAccountWaitCriterion` which checks that the given
// UserAccount has the expected spec
func UntilUserAccountHasSpec(expected toolchainv1alpha1.UserAccountSpec) UserAccountWaitCriterion {
	return UserAccountWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserAccount) bool {
			userAccount := actual.DeepCopy()
			expectedSpec := expected.DeepCopy()
			return reflect.DeepEqual(&userAccount.Spec, expectedSpec)
		},
		Diff: func(actual *toolchainv1alpha1.UserAccount) string {
			userAccount := actual.DeepCopy()
			expectedSpec := expected.DeepCopy()
			return fmt.Sprintf("expected specs to match: %s", Diff(expectedSpec, userAccount.Spec))
		},
	}
}

// UntilUserAccountMatchesMur returns a `UserAccountWaitCriterion` which loads the existing MUR
// and compares the first UserAccountSpecEmbedded in the MUR with the actual UserAccount spec
func UntilUserAccountMatchesMur(hostAwaitility *HostAwaitility) UserAccountWaitCriterion {
	return UserAccountWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserAccount) bool {
			mur, err := hostAwaitility.GetMasterUserRecord(actual.Name)
			if err != nil {
				return false
			}
			return actual.Spec.UserID == mur.Spec.UserID &&
				actual.Spec.Disabled == mur.Spec.Disabled
		},
		Diff: func(actual *toolchainv1alpha1.UserAccount) string {
			mur, err := hostAwaitility.GetMasterUserRecord(actual.Name)
			if err != nil {
				return fmt.Sprintf("could not find mur for user account '%s'", actual.Name)
			}
			return fmt.Sprintf("expected mur to match with useraccount:\n\tUserID: %s/%s\n\tDisabled: %t/%t\n", actual.Spec.UserID, mur.Spec.UserID, actual.Spec.Disabled, mur.Spec.Disabled)
		},
	}
}

// UntilUserAccountHasConditions returns a `UserAccountWaitCriterion` which checks that the given
// USerAccount has exactly all the given status conditions
func UntilUserAccountHasConditions(expected ...toolchainv1alpha1.Condition) UserAccountWaitCriterion {
	return UserAccountWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserAccount) bool {
			return test.ConditionsMatch(actual.Status.Conditions, expected...)
		},
		Diff: func(actual *toolchainv1alpha1.UserAccount) string {
			return fmt.Sprintf("expected conditions to match: %s", Diff(expected, actual.Status.Conditions))
		},
	}
}

// UntilUserAccountContainsCondition returns a `UserAccountWaitCriterion` which checks that the given
// USerAccount contains the given condition
func UntilUserAccountContainsCondition(expected toolchainv1alpha1.Condition) UserAccountWaitCriterion {
	return UserAccountWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserAccount) bool {
			return test.ContainsCondition(actual.Status.Conditions, expected)
		},
		Diff: func(actual *toolchainv1alpha1.UserAccount) string {
			e, _ := yaml.Marshal(expected)
			a, _ := yaml.Marshal(actual.Status.Conditions)
			return fmt.Sprintf("expected conditions to contain: %s.\n\tactual: %s", e, a)
		},
	}
}

// UntilUserAccountIsBeingDeleted returns a `UserAccountWaitCriterion` which checks that the given
// UserAccount has the deletion timestamp set
func UntilUserAccountIsBeingDeleted() UserAccountWaitCriterion {
	return UserAccountWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserAccount) bool {
			return actual.DeletionTimestamp != nil
		},
	}
}

// UntilUserAccountIsCreatedAfter returns a `UserAccountWaitCriterion` which checks that the given
// UserAccount has a creation timestamp that is after the given timestamp
func UntilUserAccountIsCreatedAfter(timestamp metav1.Time) UserAccountWaitCriterion {
	return UserAccountWaitCriterion{
		Match: func(actual *toolchainv1alpha1.UserAccount) bool {
			return actual.CreationTimestamp.After(timestamp.Time)
		},
	}
}

// WaitForUserAccount waits until there is a UserAccount available with the given name, expected spec and the set of status conditions
func (a *MemberAwaitility) WaitForUserAccount(t *testing.T, name string, criteria ...UserAccountWaitCriterion) (*toolchainv1alpha1.UserAccount, error) {
	var userAccount *toolchainv1alpha1.UserAccount
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.UserAccount{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		userAccount = obj
		return matchUserAccountWaitCriterion(obj, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printUserAccountWaitCriterionDiffs(t, userAccount, criteria...)
	}
	return userAccount, err
}

// SpaceRequestWaitCriterion a struct to compare with a given SpaceRequest
type SpaceRequestWaitCriterion struct {
	Match func(request *toolchainv1alpha1.SpaceRequest) bool
	Diff  func(request *toolchainv1alpha1.SpaceRequest) string
}

// WaitForSpaceRequest waits until there is a SpaceRequest available with the given name, namespace, spec and the set of status conditions
func (a *MemberAwaitility) WaitForSpaceRequest(t *testing.T, namespacedName types.NamespacedName, criteria ...SpaceRequestWaitCriterion) (*toolchainv1alpha1.SpaceRequest, error) {
	var spaceRequest *toolchainv1alpha1.SpaceRequest
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.SpaceRequest{}
		if err := a.Client.Get(context.TODO(), namespacedName, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		spaceRequest = obj
		return matchSpaceRequestWaitCriterion(obj, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printSpaceRequestWaitCriterionDiffs(t, spaceRequest, criteria...)
	}
	return spaceRequest, err
}

// UntilSpaceRequestHasTierName returns a `SpaceRequestWaitCriterion` which checks that the given
// SpaceRequest has a specific tierName
func UntilSpaceRequestHasTierName(expected string) SpaceRequestWaitCriterion {
	return SpaceRequestWaitCriterion{
		Match: func(actual *toolchainv1alpha1.SpaceRequest) bool {
			return actual.Spec.TierName == expected
		},
		Diff: func(actual *toolchainv1alpha1.SpaceRequest) string {
			return fmt.Sprintf("expected tier name to be '%s'\nbut it was '%s'", expected, actual.Spec.TierName)
		},
	}
}

// UntilSpaceRequestHasTargetClusterRoles returns a `SpaceRequestWaitCriterion` which checks that the given
// SpaceRequest has the expected cluster roles
func UntilSpaceRequestHasTargetClusterRoles(expected []string) SpaceRequestWaitCriterion {
	return SpaceRequestWaitCriterion{
		Match: func(actual *toolchainv1alpha1.SpaceRequest) bool {
			return reflect.DeepEqual(expected, actual.Spec.TargetClusterRoles)
		},
		Diff: func(actual *toolchainv1alpha1.SpaceRequest) string {
			return fmt.Sprintf("expected space roles to match:\n%s", Diff(expected, actual.Spec.TargetClusterRoles))
		},
	}
}

// UntilSpaceRequestHasStatusTargetClusterURL returns a `SpaceRequestWaitCriterion` which checks that the given
// SpaceRequest has the expected .Status.TargetClusterURL value
func UntilSpaceRequestHasStatusTargetClusterURL(expected string) SpaceRequestWaitCriterion {
	return SpaceRequestWaitCriterion{
		Match: func(actual *toolchainv1alpha1.SpaceRequest) bool {
			return expected == actual.Status.TargetClusterURL
		},
		Diff: func(actual *toolchainv1alpha1.SpaceRequest) string {
			return fmt.Sprintf("expected target cluster URL to match:\n%s", Diff(expected, actual.Status.TargetClusterURL))
		},
	}
}

// UntilSpaceRequestHasNamespaceAccess returns a `SpaceRequestWaitCriterion` which checks that the given
// SpaceRequest has `status.NamespaceAccess` set and each namespace access is actually valid.
func UntilSpaceRequestHasNamespaceAccess(subSpace *toolchainv1alpha1.Space) SpaceRequestWaitCriterion {
	var expectedNames []string
	for _, expectedNamespace := range subSpace.Status.ProvisionedNamespaces {
		expectedNames = append(expectedNames, expectedNamespace.Name)
	}
	return SpaceRequestWaitCriterion{
		Match: func(actual *toolchainv1alpha1.SpaceRequest) bool {
			// check if expected number of namespaces matches
			if len(actual.Status.NamespaceAccess) != len(expectedNames) {
				return false
			}

			for _, nsAccess := range actual.Status.NamespaceAccess {
				// check the name of the namespaces are matching
				for _, expectedNamespace := range expectedNames {
					found := false
					if expectedNamespace == nsAccess.Name {
						found = true
						break
					}
					if !found {
						return false
					}
				}
			}
			return true
		},
		Diff: func(actual *toolchainv1alpha1.SpaceRequest) string {
			if len(actual.Status.NamespaceAccess) != len(subSpace.Status.ProvisionedNamespaces) {
				return fmt.Sprintf("invalid number of namespaces found in namespaceAccess. expected %d but got %d", len(subSpace.Status.ProvisionedNamespaces), len(actual.Status.NamespaceAccess))
			}
			var actualNamespaces []string
			for _, actualNamespace := range actual.Status.NamespaceAccess {
				actualNamespaces = append(actualNamespaces, actualNamespace.Name)
			}
			return fmt.Sprintf("could not match namespace names: \n%s ", Diff(expectedNames, actualNamespaces))
		},
	}
}

// UntilSpaceRequestHasConditions returns a `SpaceRequestWaitCriterion` which checks that the given
// SpaceRequest has exactly all the given status conditions
func UntilSpaceRequestHasConditions(expected ...toolchainv1alpha1.Condition) SpaceRequestWaitCriterion {
	return SpaceRequestWaitCriterion{
		Match: func(actual *toolchainv1alpha1.SpaceRequest) bool {
			return test.ConditionsMatch(actual.Status.Conditions, expected...)
		},
		Diff: func(actual *toolchainv1alpha1.SpaceRequest) string {
			return fmt.Sprintf("expected conditions to match:\n%s", Diff(expected, actual.Status.Conditions))
		},
	}
}

func matchSpaceRequestWaitCriterion(actual *toolchainv1alpha1.SpaceRequest, criteria ...SpaceRequestWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *MemberAwaitility) printSpaceRequestWaitCriterionDiffs(t *testing.T, actual *toolchainv1alpha1.SpaceRequest, criteria ...SpaceRequestWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find SpaceRequest\n")
		buf.WriteString(a.listAndReturnContent("SpaceRequest", a.Namespace, &toolchainv1alpha1.SpaceRequestList{}))
	} else {
		buf.WriteString("failed to find SpaceRequest with matching criteria:\n")
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) && c.Diff != nil {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	t.Log(buf.String())
}

// SpaceBindingRequestWaitCriterion a struct to compare with a given SpaceBindingRequest
type SpaceBindingRequestWaitCriterion struct {
	Match func(request *toolchainv1alpha1.SpaceBindingRequest) bool
	Diff  func(request *toolchainv1alpha1.SpaceBindingRequest) string
}

// WaitForSpaceBindingRequest waits until there is a SpaceBindingRequest available with the given name, namespace, spec and the set of status conditions
func (a *MemberAwaitility) WaitForSpaceBindingRequest(t *testing.T, namespacedName types.NamespacedName, criteria ...SpaceBindingRequestWaitCriterion) (*toolchainv1alpha1.SpaceBindingRequest, error) {
	var spaceBindingRequest *toolchainv1alpha1.SpaceBindingRequest
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.SpaceBindingRequest{}
		if err := a.Client.Get(context.TODO(), namespacedName, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		spaceBindingRequest = obj
		return matchSpaceBindingRequestWaitCriterion(obj, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printSpaceBindingRequestWaitCriterionDiffs(t, spaceBindingRequest, criteria...)
	}
	return spaceBindingRequest, err
}

// UntilSpaceBindingRequestHasConditions returns a `SpaceBindingRequestWaitCriterion` which checks that the given
// SpaceBindingRequest has exactly all the given status conditions
func UntilSpaceBindingRequestHasConditions(expected ...toolchainv1alpha1.Condition) SpaceBindingRequestWaitCriterion {
	return SpaceBindingRequestWaitCriterion{
		Match: func(actual *toolchainv1alpha1.SpaceBindingRequest) bool {
			return test.ConditionsMatch(actual.Status.Conditions, expected...)
		},
		Diff: func(actual *toolchainv1alpha1.SpaceBindingRequest) string {
			return fmt.Sprintf("expected conditions to match:\n%s", Diff(expected, actual.Status.Conditions))
		},
	}
}

// UntilSpaceBindingRequestHasSpecSpaceRole returns a `SpaceBindingRequestWaitCriterion` which checks that the given
// SpaceBindingRequest has a specific .Spec.SpaceRole
func UntilSpaceBindingRequestHasSpecSpaceRole(expected string) SpaceBindingRequestWaitCriterion {
	return SpaceBindingRequestWaitCriterion{
		Match: func(actual *toolchainv1alpha1.SpaceBindingRequest) bool {
			return actual.Spec.SpaceRole == expected
		},
		Diff: func(actual *toolchainv1alpha1.SpaceBindingRequest) string {
			return fmt.Sprintf("expected SpaceRole to be '%s'\nbut it was '%s'", expected, actual.Spec.SpaceRole)
		},
	}
}

// UntilSpaceBindingRequestHasSpecMUR returns a `SpaceBindingRequestWaitCriterion` which checks that the given
// SpaceBindingRequest has a specific .Spec.MasterUserRecord
func UntilSpaceBindingRequestHasSpecMUR(expected string) SpaceBindingRequestWaitCriterion {
	return SpaceBindingRequestWaitCriterion{
		Match: func(actual *toolchainv1alpha1.SpaceBindingRequest) bool {
			return actual.Spec.MasterUserRecord == expected
		},
		Diff: func(actual *toolchainv1alpha1.SpaceBindingRequest) string {
			return fmt.Sprintf("expected MasterUserRecord to be '%s'\nbut it was '%s'", expected, actual.Spec.MasterUserRecord)
		},
	}
}

func matchSpaceBindingRequestWaitCriterion(actual *toolchainv1alpha1.SpaceBindingRequest, criteria ...SpaceBindingRequestWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *MemberAwaitility) printSpaceBindingRequestWaitCriterionDiffs(t *testing.T, actual *toolchainv1alpha1.SpaceBindingRequest, criteria ...SpaceBindingRequestWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find SpaceBindingRequest\n")
		buf.WriteString(a.listAndReturnContent("SpaceBindingRequest", a.Namespace, &toolchainv1alpha1.SpaceBindingRequestList{}))
	} else {
		buf.WriteString("failed to find SpaceBindingRequest with matching criteria:\n")
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual) && c.Diff != nil {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	t.Log(buf.String())
}

func (a *MemberAwaitility) ListSpaceBindingRequests(namespace string) ([]toolchainv1alpha1.SpaceBindingRequest, error) {
	bindings := &toolchainv1alpha1.SpaceBindingRequestList{}
	if err := a.Client.List(context.TODO(), bindings, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	return bindings.Items, nil
}

// NSTemplateSetWaitCriterion a struct to compare with a given NSTemplateSet
type NSTemplateSetWaitCriterion struct {
	Match func(*toolchainv1alpha1.NSTemplateSet) bool
	Diff  func(*toolchainv1alpha1.NSTemplateSet) string
}

func matchNSTemplateSetWaitCriterion(actual *toolchainv1alpha1.NSTemplateSet, criteria ...NSTemplateSetWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *MemberAwaitility) printNSTemplateSetWaitCriterionDiffs(t *testing.T, actual *toolchainv1alpha1.NSTemplateSet, criteria ...NSTemplateSetWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find NSTemplateSet at all\n")
		buf.WriteString(a.listAndReturnContent("NSTemplateSet", a.Namespace, &toolchainv1alpha1.NSTemplateSetList{}))
	} else {
		buf.WriteString(fmt.Sprintf("failed to find NSTemplateSet with matching criteria after %fs:\n", a.Timeout.Seconds()))
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
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
	t.Log(buf.String())
}

// UntilNSTemplateSetHasNoOwnerReferences returns a `NSTemplateSetWaitCriterion` which checks that the given
// NSTemplateSet has no Owner References
func UntilNSTemplateSetHasNoOwnerReferences() NSTemplateSetWaitCriterion {
	return NSTemplateSetWaitCriterion{
		Match: func(actual *toolchainv1alpha1.NSTemplateSet) bool {
			return len(actual.OwnerReferences) == 0
		},
		Diff: func(actual *toolchainv1alpha1.NSTemplateSet) string {
			return fmt.Sprintf("expected no owner refs: %v", actual.OwnerReferences)
		},
	}
}

// UntilNSTemplateSetIsBeingDeleted returns a `NSTemplateSetWaitCriterion` which checks that the given
// NSTemplateSet has Deletion Timestamp set
func UntilNSTemplateSetIsBeingDeleted() NSTemplateSetWaitCriterion {
	return NSTemplateSetWaitCriterion{
		Match: func(actual *toolchainv1alpha1.NSTemplateSet) bool {
			return actual.DeletionTimestamp != nil
		},
		Diff: func(_ *toolchainv1alpha1.NSTemplateSet) string {
			return "expected deletion timestamp to be set"
		},
	}
}

// UntilNSTemplateSetHasConditions returns a `NSTemplateSetWaitCriterion` which checks that the given
// NSTemlateSet has exactly all the given status conditions
func UntilNSTemplateSetHasConditions(expected ...toolchainv1alpha1.Condition) NSTemplateSetWaitCriterion {
	return NSTemplateSetWaitCriterion{
		Match: func(actual *toolchainv1alpha1.NSTemplateSet) bool {
			return test.ConditionsMatch(actual.Status.Conditions, expected...)
		},
		Diff: func(actual *toolchainv1alpha1.NSTemplateSet) string {
			return fmt.Sprintf("expected conditions to match:\n%s", Diff(expected, actual.Status.Conditions))
		},
	}
}

// UntilNSTemplateSetHasSpaceRoles returns a `NSTemplateSetWaitCriterion` which checks that the given
// NSTemlateSet has the expected roles for the given users
func UntilNSTemplateSetHasSpaceRoles(expected ...toolchainv1alpha1.NSTemplateSetSpaceRole) NSTemplateSetWaitCriterion {
	return NSTemplateSetWaitCriterion{
		Match: func(actual *toolchainv1alpha1.NSTemplateSet) bool {
			return reflect.DeepEqual(expected, actual.Spec.SpaceRoles)
		},
		Diff: func(actual *toolchainv1alpha1.NSTemplateSet) string {
			return fmt.Sprintf("expected space roles to match:\n%s", Diff(expected, actual.Spec.SpaceRoles))
		},
	}
}

// UntilNSTemplateSetHasSpaceRolesFromBindings returns a `NSTemplateSetWaitCriterion` which checks that the given
// NSTemlateSet has the expected roles for the given users
func UntilNSTemplateSetHasSpaceRolesFromBindings(tier *toolchainv1alpha1.NSTemplateTier, bindings []toolchainv1alpha1.SpaceBinding) NSTemplateSetWaitCriterion {
	expected := []toolchainv1alpha1.NSTemplateSetSpaceRole{}
	for role, tmpl := range tier.Spec.SpaceRoles {
		spaceRole := toolchainv1alpha1.NSTemplateSetSpaceRole{
			TemplateRef: tmpl.TemplateRef,
			Usernames:   []string{},
		}
		for _, b := range bindings {
			if b.Spec.SpaceRole == role {
				spaceRole.Usernames = append(spaceRole.Usernames, b.Spec.MasterUserRecord)
			}
		}
		if len(spaceRole.Usernames) > 0 {
			expected = append(expected, spaceRole)
		}
	}
	return NSTemplateSetWaitCriterion{
		Match: func(actual *toolchainv1alpha1.NSTemplateSet) bool {
			return reflect.DeepEqual(expected, actual.Spec.SpaceRoles)
		},
		Diff: func(actual *toolchainv1alpha1.NSTemplateSet) string {
			return fmt.Sprintf("expected space roles to match:\n%s", Diff(expected, actual.Spec.SpaceRoles))
		},
	}
}

// UntilNSTemplateSetHasAnySpaceRoles returns a `NSTemplateSetWaitCriterion` which checks that the given
// NSTemplateSet has any space roles set.
func UntilNSTemplateSetHasAnySpaceRoles() NSTemplateSetWaitCriterion {
	return NSTemplateSetWaitCriterion{
		Match: func(actual *toolchainv1alpha1.NSTemplateSet) bool {
			return len(actual.Spec.SpaceRoles) > 0
		},
		Diff: func(actual *toolchainv1alpha1.NSTemplateSet) string {
			return "expected space roles to not be empty."
		},
	}
}

func SpaceRole(templateRef string, usernames ...string) toolchainv1alpha1.NSTemplateSetSpaceRole {
	sort.Strings(usernames)
	return toolchainv1alpha1.NSTemplateSetSpaceRole{
		TemplateRef: templateRef,
		Usernames:   usernames,
	}
}

// UntilNSTemplateSetHasTier checks if the NSTemplateTier has the expected tierName
func UntilNSTemplateSetHasTier(expected string) NSTemplateSetWaitCriterion {
	return NSTemplateSetWaitCriterion{
		Match: func(actual *toolchainv1alpha1.NSTemplateSet) bool {
			return actual.Spec.TierName == expected
		},
		Diff: func(actual *toolchainv1alpha1.NSTemplateSet) string {
			return fmt.Sprintf("expected tier name to be '%s'\nbut it was '%s'", expected, actual.Spec.TierName)
		},
	}
}

// UntilNSTemplateSetHasProvisionedNamespaces returns a `NSTemplateSetWaitCriterion` which checks that the given
// NSTemplateSet has exactly all the given status provisioned namespaces
func UntilNSTemplateSetHasProvisionedNamespaces(expected []toolchainv1alpha1.SpaceNamespace) NSTemplateSetWaitCriterion {
	return NSTemplateSetWaitCriterion{
		Match: func(actual *toolchainv1alpha1.NSTemplateSet) bool {
			return reflect.DeepEqual(actual.Status.ProvisionedNamespaces, expected)
		},
		Diff: func(actual *toolchainv1alpha1.NSTemplateSet) string {
			return fmt.Sprintf("expected conditions to match:\n%s", Diff(expected, actual.Status.ProvisionedNamespaces))
		},
	}
}

// WaitForNSTmplSet wait until the NSTemplateSet with the given name and conditions exists
func (a *MemberAwaitility) WaitForNSTmplSet(t *testing.T, name string, criteria ...NSTemplateSetWaitCriterion) (*toolchainv1alpha1.NSTemplateSet, error) {
	t.Logf("waiting for NSTemplateSet '%s' to match criteria", name)
	var nsTmplSet *toolchainv1alpha1.NSTemplateSet
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.NSTemplateSet{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: a.Namespace}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		nsTmplSet = obj
		return matchNSTemplateSetWaitCriterion(obj, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printNSTemplateSetWaitCriterionDiffs(t, nsTmplSet, criteria...)
	}
	return nsTmplSet, err
}

// WaitUntilNSTemplateSetDeleted waits until the NSTemplateSet with the given name is deleted (ie, is not found)
func (a *MemberAwaitility) WaitUntilNSTemplateSetDeleted(t *testing.T, name string) error {
	t.Logf("waiting for until NSTemplateSet '%s' in namespace '%s' is deleted", name, a.Namespace)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		nsTmplSet := &toolchainv1alpha1.NSTemplateSet{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: a.Namespace}, nsTmplSet); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

type NamespaceWaitCriterion struct {
	Match func(*corev1.Namespace) bool
	Diff  func(*corev1.Namespace) string
}

type LabelWaitCriterion struct {
	Match func(metav1.ObjectMeta) bool
	Diff  func(metav1.ObjectMeta) string
}

// UntilNamespaceIsActive returns a `NamespaceWaitCriterion` which checks that the given
// Namespace is in `Active` phase
func UntilNamespaceIsActive() NamespaceWaitCriterion {
	return NamespaceWaitCriterion{
		Match: func(actual *corev1.Namespace) bool {
			return actual.Status.Phase == corev1.NamespaceActive
		},
		Diff: func(actual *corev1.Namespace) string {
			return fmt.Sprintf("expected namespace to be active:\n%s", actual.Status.Phase)
		},
	}
}

// UntilObjectHasLabel returns a `LabelWaitCriterion` which checks that the given Object has the expected label
func UntilObjectHasLabel(labelKey, labelValue string) LabelWaitCriterion {
	return LabelWaitCriterion{
		Match: func(actual metav1.ObjectMeta) bool {
			return actual.Labels[labelKey] == labelValue
		},
		Diff: func(actual metav1.ObjectMeta) string {
			return fmt.Sprintf("expected object to be match label,\nExpected: %s:%s\nActual labels:%v", labelKey, labelValue, actual.Labels)
		},
	}
}

// UntilNamespaceIsActive returns a `NamespaceWaitCriterion` which checks that the given
// Namespace is in `Active` phase
func UntilHasLastAppliedSpaceRoles(expected []toolchainv1alpha1.NSTemplateSetSpaceRole) NamespaceWaitCriterion {
	expectedLastAppliedSpaceRoles, _ := json.Marshal(expected) // nolint:errchkjson // assume that encoding always works
	return NamespaceWaitCriterion{
		Match: func(actual *corev1.Namespace) bool {
			lastAppliedSpaceRoles, found := actual.Annotations[toolchainv1alpha1.LastAppliedSpaceRolesAnnotationKey]
			if !found {
				return false
			}

			return string(expectedLastAppliedSpaceRoles) == lastAppliedSpaceRoles
		},
		Diff: func(actual *corev1.Namespace) string {
			return fmt.Sprintf("expected namespace to match annotation,\nExpected: %s\nActual annotations:%v", expectedLastAppliedSpaceRoles, actual.Annotations)
		},
	}
}

func matchNamespaceWaitCriteria(actual *corev1.Namespace, criteria ...NamespaceWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

// WaitForNamespace waits until a namespace with the given owner (username), type, revision and tier labels exists
func (a *MemberAwaitility) WaitForNamespace(t *testing.T, owner, tmplRef, tierName string, criteria ...NamespaceWaitCriterion) (*corev1.Namespace, error) {
	_, kind, err := TierAndType(tmplRef)
	if err != nil {
		return nil, err
	}
	labels := map[string]string{
		toolchainv1alpha1.SpaceLabelKey:       owner,
		toolchainv1alpha1.TemplateRefLabelKey: tmplRef,
		toolchainv1alpha1.TierLabelKey:        tierName,
		toolchainv1alpha1.TypeLabelKey:        kind,
		toolchainv1alpha1.ProviderLabelKey:    toolchainv1alpha1.ProviderLabelValue,
	}
	t.Logf("waiting for namespace with custom criteria and labels %v", labels)
	var ns *corev1.Namespace
	err = wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		nss := &corev1.NamespaceList{}
		opts := client.MatchingLabels(labels)
		if err := a.Client.List(context.TODO(), nss, opts); err != nil {
			return false, err
		}
		if len(nss.Items) != 1 {
			return false, nil
		}
		ns = &nss.Items[0]
		return matchNamespaceWaitCriteria(ns, criteria...), nil
	})
	if err != nil {
		t.Logf("failed to wait for namespace with labels: %v", labels)
		opts := client.MatchingLabels(map[string]string{
			toolchainv1alpha1.ProviderLabelKey: toolchainv1alpha1.ProviderLabelValue,
		})
		a.listAndPrint(t, "Namespaces", "", &corev1.NamespaceList{}, opts)
		if ns == nil {
			t.Logf("a namespace with the following labels was not found: %v", labels)
			return nil, err
		}
		for _, c := range criteria {
			t.Logf(c.Diff(ns))
		}
		return nil, err
	}
	t.Logf("found namespace %s with custom criteria and labels %v", ns.Name, labels)
	return ns, nil
}

// WaitForNamespaceWithName waits until a namespace with the given name
func (a *MemberAwaitility) WaitForNamespaceWithName(t *testing.T, name string, criteria ...LabelWaitCriterion) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &corev1.Namespace{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		ns = obj
		return matchLabelWaitCriteria(ns.ObjectMeta, criteria...), nil
	})
	if err != nil {
		t.Log("failed to wait for namespace")
		a.printNamespaceLabelCriterionDiffs(t, ns, criteria...)
		return nil, err
	}
	return ns, nil
}

func matchLabelWaitCriteria(actual metav1.ObjectMeta, criteria ...LabelWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *MemberAwaitility) printNamespaceLabelCriterionDiffs(t *testing.T, actual *corev1.Namespace, criteria ...LabelWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find Namespace\n")
		buf.WriteString(a.listAndReturnContent("Namespace", "", &corev1.NamespaceList{}))
	} else {
		buf.WriteString("failed to find Namespace with matching label criteria:\n")
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual.ObjectMeta) {
				buf.WriteString(c.Diff(actual.ObjectMeta))
				buf.WriteString("\n")
			}
		}
	}
	t.Log(buf.String())
}

// WaitForNamespaceInTerminating waits until a namespace with the given name has a deletion timestamp and in Terminating Phase
func (a *MemberAwaitility) WaitForNamespaceInTerminating(t *testing.T, nsName string) (*corev1.Namespace, error) {
	ns := &corev1.Namespace{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &corev1.Namespace{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: nsName}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		ns = obj
		return obj.DeletionTimestamp != nil && obj.Status.Phase == corev1.NamespaceTerminating, nil
	})
	if err != nil {
		t.Logf("failed to wait for namespace '%s' to be in 'Terminating' phase", nsName)
		return nil, err
	}
	return ns, nil
}

func (a *MemberAwaitility) printRoleBindingWaitCriterionDiffs(t *testing.T, actual *rbacv1.RoleBinding, criteria ...LabelWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find RoleBinding\n")
		buf.WriteString(a.listAndReturnContent("RoleBinding", "", &rbacv1.RoleBindingList{}))
	} else {
		buf.WriteString("failed to find RoleBinding with matching criteria:\n")
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual.ObjectMeta) {
				buf.WriteString(c.Diff(actual.ObjectMeta))
				buf.WriteString("\n")
			}
		}
	}
	t.Log(buf.String())
}

// WaitForRoleBinding waits until a RoleBinding with the given name exists in the given namespace
func (a *MemberAwaitility) WaitForRoleBinding(t *testing.T, namespace *corev1.Namespace, name string, criteria ...LabelWaitCriterion) (*rbacv1.RoleBinding, error) {
	t.Logf("waiting for RoleBinding '%s' in namespace '%s'", name, namespace.Name)
	roleBinding := &rbacv1.RoleBinding{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &rbacv1.RoleBinding{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace.Name, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		roleBinding = obj
		return matchLabelWaitCriteria(obj.ObjectMeta, criteria...), nil
	})
	if err != nil {
		t.Logf("failed to wait for rolebinding")
		a.printRoleBindingWaitCriterionDiffs(t, roleBinding, criteria...)
		return nil, err
	}
	t.Logf("found rolebinding %s with custom labels", roleBinding.Name)
	return roleBinding, err
}

// WaitUntilRoleBindingDeleted waits until a RoleBinding with the given name does not exist anymore in the given namespace
func (a *MemberAwaitility) WaitUntilRoleBindingDeleted(t *testing.T, namespace *corev1.Namespace, name string) error {
	t.Logf("waiting for RoleBinding '%s' in namespace '%s' to be deleted", name, namespace.Name)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		roleBinding := &rbacv1.RoleBinding{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: a.Namespace}, roleBinding); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

func (a *MemberAwaitility) WaitForServiceAccount(t *testing.T, namespace string, name string, criteria ...LabelWaitCriterion) (*corev1.ServiceAccount, error) {
	t.Logf("waiting for ServiceAccount '%s' in namespace '%s'", name, namespace)
	serviceAccount := &corev1.ServiceAccount{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &corev1.ServiceAccount{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		serviceAccount = obj
		return matchLabelWaitCriteria(obj.ObjectMeta, criteria...), nil
	})
	if err != nil {
		t.Logf("failed to wait for ServiceAccount '%s' in namespace '%s'.", name, namespace)
		return nil, err
	}
	return serviceAccount, err
}

// WaitForLimitRange waits until a LimitRange with the given name exists in the given namespace
func (a *MemberAwaitility) WaitForLimitRange(t *testing.T, namespace *corev1.Namespace, name string) (*corev1.LimitRange, error) {
	t.Logf("waiting for LimitRange '%s' in namespace '%s'", name, namespace.Name)
	lr := &corev1.LimitRange{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &corev1.LimitRange{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace.Name, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				allLRs := &corev1.LimitRangeList{}
				if err := a.Client.List(context.TODO(), allLRs, client.MatchingLabels(codereadyToolchainProviderLabel)); err != nil {
					return false, err
				}
				return false, nil
			}
			return false, err
		}
		lr = obj
		return true, nil
	})
	if err != nil {
		t.Logf("failed to wait for LimitRange '%s' in namespace '%s'", name, namespace.Name)
	}
	return lr, err
}

// WaitForNetworkPolicy waits until a NetworkPolicy with the given name exists in the given namespace
func (a *MemberAwaitility) WaitForNetworkPolicy(t *testing.T, namespace *corev1.Namespace, name string) (*netv1.NetworkPolicy, error) {
	t.Logf("waiting for NetworkPolicy '%s' in namespace '%s'", name, namespace.Name)
	np := &netv1.NetworkPolicy{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &netv1.NetworkPolicy{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace.Name, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				allNPs := &netv1.NetworkPolicyList{}
				if err := a.Client.List(context.TODO(), allNPs, client.MatchingLabels(codereadyToolchainProviderLabel)); err != nil {
					return false, err
				}
				return false, nil
			}
			return false, err
		}
		np = obj
		return true, nil
	})
	if err != nil {
		t.Logf("failed to wait for NetworkPolicy '%s' in namespace '%s'", name, namespace.Name)
	}
	return np, err
}

// WaitForRole waits until a Role with the given name exists in the given namespace
func (a *MemberAwaitility) WaitForRole(t *testing.T, namespace *corev1.Namespace, name string, criteria ...LabelWaitCriterion) (*rbacv1.Role, error) {
	t.Logf("waiting for Role '%s' in namespace '%s'", name, namespace.Name)
	role := &rbacv1.Role{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &rbacv1.Role{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace.Name, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		role = obj
		return matchLabelWaitCriteria(obj.ObjectMeta, criteria...), nil
	})
	if err != nil {
		t.Logf("failed to wait for Role '%s' in namespace '%s'", name, namespace.Name)
		a.printRoleWaitCriterionDiffs(t, role, criteria...)
	}
	return role, err
}

// WaitUntilRoleDeleted waits until a Role with the given name does not exist anymore in the given namespace
func (a *MemberAwaitility) WaitUntilRoleDeleted(t *testing.T, namespace *corev1.Namespace, name string) error {
	t.Logf("waiting for Role '%s' in namespace '%s' to be deleted", name, namespace.Name)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		role := &rbacv1.Role{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name, Namespace: a.Namespace}, role); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

func (a *MemberAwaitility) printRoleWaitCriterionDiffs(t *testing.T, actual *rbacv1.Role, criteria ...LabelWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find Role\n")
		buf.WriteString(a.listAndReturnContent("Role", "", &rbacv1.RoleList{}))
	} else {
		buf.WriteString("failed to find Role with matching criteria:\n")
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual.ObjectMeta) {
				buf.WriteString(c.Diff(actual.ObjectMeta))
				buf.WriteString("\n")
			}
		}
	}
	t.Log(buf.String())
}

// ClusterResourceQuotaWaitCriterion a struct to compare with a given ClusterResourceQuota
type ClusterResourceQuotaWaitCriterion struct {
	Match func(*quotav1.ClusterResourceQuota) bool
	Diff  func(*quotav1.ClusterResourceQuota) string
}

func matchClusterResourceQuotaWaitCriteria(actual *quotav1.ClusterResourceQuota, criteria ...ClusterResourceQuotaWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *MemberAwaitility) printClusterResourceQuotaWaitCriterionDiffs(t *testing.T, actual *quotav1.ClusterResourceQuota, criteria ...ClusterResourceQuotaWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find ClusterResourceQuota\n")
		buf.WriteString(a.listAndReturnContent("ClusterResourceQuota", "", &quotav1.ClusterResourceQuotaList{}))
	} else {
		buf.WriteString("failed to find ClusterResourceQuota with matching criteria:\n")
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
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
	t.Log(buf.String())
}

// WaitForClusterResourceQuota waits until a ClusterResourceQuota with the given name exists
func (a *MemberAwaitility) WaitForClusterResourceQuota(t *testing.T, name string, criteria ...ClusterResourceQuotaWaitCriterion) (*quotav1.ClusterResourceQuota, error) {
	t.Logf("waiting for ClusterResourceQuota '%s' to match criteria", name)
	quota := &quotav1.ClusterResourceQuota{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &quotav1.ClusterResourceQuota{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				quotaList := &quotav1.ClusterResourceQuotaList{}
				ls := codereadyToolchainProviderLabel
				if err := a.Client.List(context.TODO(), quotaList, client.MatchingLabels(ls)); err != nil {
					return false, err
				}
				return false, nil
			}
			return false, err
		}
		quota = obj
		return matchClusterResourceQuotaWaitCriteria(obj, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printClusterResourceQuotaWaitCriterionDiffs(t, quota, criteria...)
	}
	return quota, err
}

// ResourceQuotaWaitCriterion a struct to compare with a given ResourceQuota
type ResourceQuotaWaitCriterion struct {
	Match func(*corev1.ResourceQuota) bool
	Diff  func(*corev1.ResourceQuota) string
}

func matchResourceQuotaWaitCriteria(actual *corev1.ResourceQuota, criteria ...ResourceQuotaWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *MemberAwaitility) printResourceQuotaWaitCriterionDiffs(t *testing.T, actual *corev1.ResourceQuota, criteria ...ResourceQuotaWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find ResourceQuota\n")
		buf.WriteString(a.listAndReturnContent("ResourceQuota", "", &corev1.ResourceQuotaList{}))
	} else {
		buf.WriteString("failed to find ResourceQuota with matching criteria:\n")
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
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
	t.Log(buf.String())
}

// WaitForResourceQuota waits until a ResourceQuota with the given name exists
func (a *MemberAwaitility) WaitForResourceQuota(t *testing.T, namespace, name string, criteria ...ResourceQuotaWaitCriterion) (*corev1.ResourceQuota, error) {
	t.Logf("waiting for ResourceQuota '%s' in %s to match criteria", name, namespace)
	quota := &corev1.ResourceQuota{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &corev1.ResourceQuota{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		quota = obj
		return matchResourceQuotaWaitCriteria(obj, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printResourceQuotaWaitCriterionDiffs(t, quota, criteria...)
	}
	return quota, err
}

// IdlerWaitCriterion a struct to compare with a given Idler
type IdlerWaitCriterion struct {
	Match func(*toolchainv1alpha1.Idler) bool
	Diff  func(*toolchainv1alpha1.Idler) string
}

func matchIdlerWaitCriteria(actual *toolchainv1alpha1.Idler, criteria ...IdlerWaitCriterion) bool {
	for _, c := range criteria {
		// if at least one criteria does not match, keep waiting
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *MemberAwaitility) printIdlerWaitCriteriaDiffs(t *testing.T, actual *toolchainv1alpha1.Idler, criteria ...IdlerWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find Idler\n")
		buf.WriteString(a.listAndReturnContent("Idler", "", &toolchainv1alpha1.IdlerList{}))
		buf.WriteString(a.listAndReturnContent("Idler", "", &toolchainv1alpha1.IdlerList{}))
	} else {
		buf.WriteString("failed to find Idler with matching criteria:\n")
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			// if at least one criteria does not match, keep waiting
			if !c.Match(actual) {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	t.Log(buf.String())
}

// IdlerConditions returns a `IdlerWaitCriterion` which checks that the given
// Idler has exactly all the given status conditions
func IdlerConditions(expected ...toolchainv1alpha1.Condition) IdlerWaitCriterion {
	return IdlerWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Idler) bool {
			return test.ConditionsMatch(actual.Status.Conditions, expected...)
		},
		Diff: func(actual *toolchainv1alpha1.Idler) string {
			return fmt.Sprintf("expected conditions to match: %s", Diff(expected, actual.Status.Conditions))
		},
	}
}

// IdlerHasTimeoutSeconds checks if the Idler has the given timeout set
func IdlerHasTimeoutSeconds(timeoutSeconds int) IdlerWaitCriterion {
	return IdlerWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Idler) bool {
			return int32(timeoutSeconds) == actual.Spec.TimeoutSeconds
		},
		Diff: func(actual *toolchainv1alpha1.Idler) string {
			return fmt.Sprintf("expected Idler timeoutSeconds to be '%d' but it was '%d'", timeoutSeconds, actual.Spec.TimeoutSeconds)
		},
	}
}

// IdlerHasTier checks if the Idler has the given tier name set as a label
func IdlerHasTier(expected string) IdlerWaitCriterion {
	return IdlerWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Idler) bool {
			return actual.Labels != nil && expected == actual.Labels["toolchain.dev.openshift.com/tier"]
		},
		Diff: func(actual *toolchainv1alpha1.Idler) string {
			return fmt.Sprintf("expected Idler 'toolchain.dev.openshift.com/tier' label to be '%s' but it was '%s'", expected, actual.Labels["toolchain.dev.openshift.com/tier"])
		},
	}
}

// IdlerHasLabel checks if the Idler has the given label
func IdlerHasLabel(key, value string) IdlerWaitCriterion {
	return IdlerWaitCriterion{
		Match: func(actual *toolchainv1alpha1.Idler) bool {
			return actual.Labels != nil && value == actual.Labels[key]
		},
		Diff: func(actual *toolchainv1alpha1.Idler) string {
			return fmt.Sprintf("expected Idler %s label to be '%s' but it was '%s'", actual.Name, value, actual.Labels[key])
		},
	}
}

// WaitForIdler waits until an Idler with the given name exists
func (a *MemberAwaitility) WaitForIdler(t *testing.T, name string, criteria ...IdlerWaitCriterion) (*toolchainv1alpha1.Idler, error) {
	t.Logf("waiting for Idler '%s' to match criteria", name)
	idler := &toolchainv1alpha1.Idler{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.Idler{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		idler = obj
		return matchIdlerWaitCriteria(obj, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printIdlerWaitCriteriaDiffs(t, idler, criteria...)
	}
	return idler, err
}

// UpdateIdlerSpec tries to update the Idler.Spec until success
func (a *MemberAwaitility) UpdateIdlerSpec(t *testing.T, idler *toolchainv1alpha1.Idler) (*toolchainv1alpha1.Idler, error) {
	var result *toolchainv1alpha1.Idler
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &toolchainv1alpha1.Idler{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: idler.Name}, obj); err != nil {
			return false, err
		}
		obj.Spec = idler.Spec
		if err := a.Client.Update(context.TODO(), obj); err != nil {
			t.Logf("trying to update Idler %s. Error: %s. Will try to update again.", idler.Name, err.Error())
			return false, nil
		}
		result = obj
		return true, nil
	})
	return result, err
}

// UpdateNamespace tries to update the Spec of the given Namespace
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and tries again
// Returns the updated Namespace
func (a *MemberAwaitility) UpdateNamespace(t *testing.T, nsName string, modifyNamespace func(ns *corev1.Namespace)) (*corev1.Namespace, error) {
	var ns *corev1.Namespace
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		freshNs := &corev1.Namespace{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: nsName}, freshNs); err != nil {
			return true, err
		}
		modifyNamespace(freshNs)
		if err := a.Client.Update(context.TODO(), freshNs); err != nil {
			t.Logf("error updating Namespace '%s': %s. Will retry again...", nsName, err.Error())
			return false, nil
		}
		ns = freshNs
		return true, nil
	})
	return ns, err
}

// UpdateServiceAccount tries to update the given ServiceAccount
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and tries again
// Returns the updated ServiceAccount
func (a *MemberAwaitility) UpdateServiceAccount(t *testing.T, namespace, saName string, modifySA func(sa *corev1.ServiceAccount)) (*corev1.ServiceAccount, error) {
	var sa *corev1.ServiceAccount
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		freshSA := &corev1.ServiceAccount{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: saName}, freshSA); err != nil {
			return true, err
		}
		modifySA(freshSA)
		if err := a.Client.Update(context.TODO(), freshSA); err != nil {
			t.Logf("error updating ServiceAccount '%s': %s. Will retry again...", saName, err.Error())
			return false, nil
		}
		sa = freshSA
		return true, nil
	})
	return sa, err
}

// UpdateSpaceRequest tries to update the Spec of the given SpaceRequest
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and tries again
// Returns the updated SpaceRequest
func (a *MemberAwaitility) UpdateSpaceRequest(t *testing.T, spaceRequestNamespacedName types.NamespacedName, modifySpaceRequest func(s *toolchainv1alpha1.SpaceRequest)) (*toolchainv1alpha1.SpaceRequest, error) {
	var sr *toolchainv1alpha1.SpaceRequest
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		freshSpaceRequest := &toolchainv1alpha1.SpaceRequest{}
		if err := a.Client.Get(context.TODO(), spaceRequestNamespacedName, freshSpaceRequest); err != nil {
			return true, err
		}
		modifySpaceRequest(freshSpaceRequest)
		if err := a.Client.Update(context.TODO(), freshSpaceRequest); err != nil {
			t.Logf("error updating SpaceRequest '%s' in namespace '%s': %s. Will retry again...", spaceRequestNamespacedName.Name, spaceRequestNamespacedName.Name, err.Error())
			return false, nil
		}
		sr = freshSpaceRequest
		return true, nil
	})
	return sr, err
}

// UpdateSpaceBindingRequest tries to update the Spec of the given SpaceBindingRequest
// If it fails with an error (for example if the object has been modified) then it retrieves the latest version and tries again
// Returns the updated SpaceBindingRequest
func (a *MemberAwaitility) UpdateSpaceBindingRequest(t *testing.T, spaceBindingRequestNamespacedName types.NamespacedName, modifySpaceBindingRequest func(s *toolchainv1alpha1.SpaceBindingRequest)) (*toolchainv1alpha1.SpaceBindingRequest, error) {
	var sr *toolchainv1alpha1.SpaceBindingRequest
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		freshSpaceBindingRequest := &toolchainv1alpha1.SpaceBindingRequest{}
		if err := a.Client.Get(context.TODO(), spaceBindingRequestNamespacedName, freshSpaceBindingRequest); err != nil {
			return true, err
		}
		modifySpaceBindingRequest(freshSpaceBindingRequest)
		if err := a.Client.Update(context.TODO(), freshSpaceBindingRequest); err != nil {
			t.Logf("error updating SpaceBindingRequest '%s' in namespace '%s': %s. Will retry again...", spaceBindingRequestNamespacedName.Name, spaceBindingRequestNamespacedName.Name, err.Error())
			return false, err
		}
		sr = freshSpaceBindingRequest
		return true, nil
	})
	return sr, err
}

// WaitUntilSpaceBindingRequestDeleted waits until a SpaceBindingRequest with the given name does not exist anymore in the given namespace
func (a *MemberAwaitility) WaitUntilSpaceBindingRequestDeleted(t *testing.T, spaceBindingRequest *toolchainv1alpha1.SpaceBindingRequest) error {
	t.Logf("waiting for SpaceBindingRequest '%s' in namespace '%s' to be deleted", spaceBindingRequest.GetName(), spaceBindingRequest.GetNamespace())
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		sbr := &toolchainv1alpha1.SpaceBindingRequest{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: spaceBindingRequest.GetName(), Namespace: spaceBindingRequest.GetNamespace()}, sbr); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

// Create tries to create the object until success
// Workaround for https://github.com/kubernetes/kubernetes/issues/67761
func (a *MemberAwaitility) Create(t *testing.T, obj client.Object) error {
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		if err := a.Client.Create(context.TODO(), obj); err != nil {
			t.Logf("trying to create %+v. Error: %s. Will try to create again.", obj, err.Error())
			return false, nil
		}
		return true, nil
	})
}

// PodWaitCriterion a struct to compare with a given Pod
type PodWaitCriterion struct {
	Match func(*corev1.Pod) bool
	Diff  func(*corev1.Pod) string
}

func matchPodWaitCriterion(actual *corev1.Pod, criteria ...PodWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *MemberAwaitility) printPodWaitCriterionDiffs(t *testing.T, actual *corev1.Pod, ns string, criteria ...PodWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find Pod\n")
		buf.WriteString(a.listAndReturnContent("Pod", ns, &corev1.PodList{}))
	} else {
		buf.WriteString("failed to find Pod with matching criteria:\n")
		for _, c := range criteria {
			if !c.Match(actual) {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	t.Log(buf.String())
}

// WaitForPod waits until a pod with the given name exists in the given namespace
func (a *MemberAwaitility) WaitForPod(t *testing.T, namespace, name string, criteria ...PodWaitCriterion) (*corev1.Pod, error) {
	t.Logf("waiting for Pod '%s' in namespace '%s' with matching criteria", name, namespace)
	var pod *corev1.Pod
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &corev1.Pod{}
		if err = a.Client.Get(context.TODO(), types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}, obj); err != nil {
			if errors.IsNotFound(err) {
				// loop again
				return false, nil
			}
			// exit
			return false, err
		}
		pod = obj
		return matchPodWaitCriterion(obj, criteria...), nil
	})
	// no match found, print the diffs
	if err != nil {
		a.printPodWaitCriterionDiffs(t, pod, namespace, criteria...)
	}
	return pod, err
}

// WaitForConfigMap waits until a ConfigMap with the given name exists in the given namespace
func (a *MemberAwaitility) WaitForConfigMap(t *testing.T, namespace, name string) (*corev1.ConfigMap, error) {
	t.Logf("waiting for ConfigMap '%s' in namespace '%s'", name, namespace)
	var cm *corev1.ConfigMap
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &corev1.ConfigMap{}
		if err = a.Client.Get(context.TODO(), types.NamespacedName{
			Namespace: namespace,
			Name:      name,
		}, obj); err != nil {
			if errors.IsNotFound(err) {
				// loop again
				return false, nil
			}
			// exit
			return false, err
		}
		cm = obj
		return true, nil
	})
	return cm, err
}

// WaitForSecret waits until a Secret with the given name exists in the operator namespace
func (a *MemberAwaitility) WaitForSecret(t *testing.T, name string) (*corev1.Secret, error) {
	t.Logf("waiting for Secret '%s' in namespace '%s'", name, a.Namespace)
	var cm *corev1.Secret
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &corev1.Secret{}
		if err = a.Client.Get(context.TODO(), types.NamespacedName{
			Namespace: a.Namespace,
			Name:      name,
		}, obj); err != nil {
			if errors.IsNotFound(err) {
				// loop again
				return false, nil
			}
			// exit
			return false, err
		}
		cm = obj
		return true, nil
	})
	return cm, err
}

// WaitForPods waits until "n" number of pods exist in the given namespace
func (a *MemberAwaitility) WaitForPods(t *testing.T, namespace string, n int, criteria ...PodWaitCriterion) ([]corev1.Pod, error) {
	t.Logf("waiting for Pods in namespace '%s' with matching criteria", namespace)
	pods := make([]corev1.Pod, 0, n)
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		pds := make([]corev1.Pod, 0, n)
		foundPods := &corev1.PodList{}
		if err := a.Client.List(context.TODO(), foundPods, client.InNamespace(namespace)); err != nil {
			return false, err
		}
	pods:
		for _, p := range foundPods.Items {
			if !matchPodWaitCriterion(&p, criteria...) { // nolint:gosec
				// skip of criteria do not match
				continue pods
			}
			pod := p // copy
			pds = append(pds, pod)
		}
		if len(pds) != n {
			return false, nil
		}
		pods = pds
		return true, nil
	})
	return pods, err
}

// WaitUntilPodsDeleted waits until the pods are deleted from the given namespace
func (a *MemberAwaitility) WaitUntilPodsDeleted(t *testing.T, namespace string, criteria ...PodWaitCriterion) error {
	t.Logf("waiting until Pods with matching criteria in namespace '%s' are deleted", namespace)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		foundPods := &corev1.PodList{}
		if err := a.Client.List(context.TODO(), foundPods, &client.ListOptions{Namespace: namespace}); err != nil {
			return false, err
		}
		if len(foundPods.Items) == 0 {
			return true, nil
		}
		for _, p := range foundPods.Items {
			if !matchPodWaitCriterion(&p, criteria...) { // nolint:gosec
				// keep waiting
				return false, nil
			}
		}
		return true, nil
	})
}

// WaitUntilPodDeleted waits until the pod with the given name is deleted from the given namespace
func (a *MemberAwaitility) WaitUntilPodDeleted(t *testing.T, namespace, name string) error {
	t.Logf("waiting until Pod '%s' in namespace '%s' is deleted", name, namespace)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &corev1.Pod{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: name}, obj); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		if util.IsBeingDeleted(obj) {
			return true, nil
		}
		return false, nil
	})
}

// PodRunning checks if the Pod in the running phase
func PodRunning() PodWaitCriterion {
	return PodWaitCriterion{
		Match: func(actual *corev1.Pod) bool {
			return actual.Status.Phase == corev1.PodRunning
		},
		Diff: func(actual *corev1.Pod) string {
			return fmt.Sprintf("expected Pod to be 'Running'\nbut it was '%s'", actual.Status.Phase)
		},
	}
}

// WithPodName checks if the Pod has the expected name
func WithPodName(expected string) PodWaitCriterion {
	return PodWaitCriterion{
		Match: func(actual *corev1.Pod) bool {
			return actual.Name == expected
		},
		Diff: func(actual *corev1.Pod) string {
			return fmt.Sprintf("expected Pod to be name '%s'\nbut it was '%s'", expected, actual.Name)
		},
	}
}

// WithPodLabel checks if the Pod has the expected label
func WithPodLabel(key, value string) PodWaitCriterion {
	return PodWaitCriterion{
		Match: func(actual *corev1.Pod) bool {
			return actual.Labels[key] == value
		},
		Diff: func(actual *corev1.Pod) string {
			return fmt.Sprintf("expected Pod label '%s' to be '%s'\nbut it was '%s'", key, value, actual.Labels[key])
		},
	}
}

func WithSandboxPriorityClass() PodWaitCriterion {
	return PodWaitCriterion{
		Match: func(actual *corev1.Pod) bool {
			return checkPriorityClass(actual, "sandbox-users-pods", -3)
		},
		Diff: func(actual *corev1.Pod) string {
			return fmt.Sprintf("expected priorityClass to be 'sandbox-users-pods'/'-3'\nbut it was '%s'/'%d'", actual.Spec.PriorityClassName, actual.Spec.Priority)
		},
	}
}

func WithOriginalPriorityClass() PodWaitCriterion {
	return PodWaitCriterion{
		Match: func(actual *corev1.Pod) bool {
			if actual.Name != "idler-test-pod-1" {
				return checkPriorityClass(actual, "", 0)
			}
			return checkPriorityClass(actual, "system-cluster-critical", 2000000000)
		},
		Diff: func(actual *corev1.Pod) string {
			if actual.Name != "idler-test-pod-1" {
				return fmt.Sprintf("expected priorityClass to be '(unamed)'/'0'\nbut it was '%s'/'%d'", actual.Spec.PriorityClassName, actual.Spec.Priority)
			}
			return fmt.Sprintf("expected priorityClass to be 'system-cluster-critical'/'2000000000'\nbut it was '%s'/'%d'", actual.Spec.PriorityClassName, actual.Spec.Priority)
		},
	}
}

func checkPriorityClass(pod *corev1.Pod, name string, priority int) bool {
	return pod.Spec.PriorityClassName == name && *pod.Spec.Priority == int32(priority)
}

// WaitUntilNamespaceDeleted waits until the namespace with the given name is deleted (ie, is not found)
func (a *MemberAwaitility) WaitUntilNamespaceDeleted(t *testing.T, username, typeName string) error {
	t.Logf("waiting until namespace for user '%s' and type '%s' is deleted", username, typeName)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		labels := map[string]string{
			toolchainv1alpha1.SpaceLabelKey: username,
			toolchainv1alpha1.TypeLabelKey:  typeName,
		}
		opts := client.MatchingLabels(labels)
		namespaceList := &corev1.NamespaceList{}
		if err := a.Client.List(context.TODO(), namespaceList, opts); err != nil {
			return false, err
		}
		if len(namespaceList.Items) < 1 {
			return true, nil
		}
		return false, nil
	})
}

// WaitUntilSecretsDeleted waits until the secrets with the given labels are deleted (ie, is not found)
func (a *MemberAwaitility) WaitUntilSecretsDeleted(t *testing.T, namespace string, labels client.MatchingLabels) error {
	t.Logf("waiting until secrets with lables '%v' in namespace '%s' is deleted", labels, namespace)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		secretList := &corev1.SecretList{}
		if err := a.Client.List(context.TODO(), secretList, labels); err != nil {
			return false, err
		}
		if len(secretList.Items) < 1 {
			return true, nil
		}
		return false, nil
	})
}

// UserWaitCriterion a struct to compare with a given User
type UserWaitCriterion struct {
	Match func(*userv1.User) bool
	Diff  func(*userv1.User) string
}

func matchUserWaitCriterion(actual *userv1.User, criteria ...UserWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *MemberAwaitility) printUserWaitCriterionDiffs(t *testing.T, actual *userv1.User, criteria ...UserWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find User\n")
		buf.WriteString(a.listAndReturnContent("User", actual.Namespace, &userv1.UserList{}))
	} else {
		buf.WriteString("failed to find User with matching criteria:\n")
		for _, c := range criteria {
			if !c.Match(actual) {
				buf.WriteString(c.Diff(actual))
				buf.WriteString("\n")
			}
		}
	}
	t.Log(buf.String())
}

// WaitForUser waits until there is a User with the given name available
func (a *MemberAwaitility) WaitForUser(t *testing.T, name string, criteria ...UserWaitCriterion) (*userv1.User, error) {
	t.Logf("waiting for User '%s'", name)
	user := &userv1.User{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		user = &userv1.User{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name}, user); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if !matchUserWaitCriterion(user, criteria...) {
			return false, nil
		}
		if user.Name != "" && len(user.Identities) > 0 {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		a.printUserWaitCriterionDiffs(t, user, criteria...)
	}
	return user, err
}

// UntilUserHasLabel checks if the User has the expected label
func UntilUserHasLabel(key, value string) UserWaitCriterion {
	return UserWaitCriterion{
		Match: func(actual *userv1.User) bool {
			return actual.Labels[key] == value
		},
		Diff: func(actual *userv1.User) string {
			return fmt.Sprintf("expected User label '%s' to be '%s'\nbut it was '%s'", key, value, actual.Labels[key])
		},
	}
}

// UntilUserHasAnnotation checks if the User has the expected annotation
func UntilUserHasAnnotation(key, value string) UserWaitCriterion {
	return UserWaitCriterion{
		Match: func(actual *userv1.User) bool {
			actualValue, exist := actual.Annotations[key]
			return exist && actualValue == value
		},
		Diff: func(actual *userv1.User) string {
			return fmt.Sprintf("expected User annotation '%s' to be '%s'\nbut it was '%s'", key, value, actual.Annotations[key])
		},
	}
}

// IdentityWaitCriterion a struct to compare with a given Identity
type IdentityWaitCriterion struct {
	Match func(*userv1.Identity) bool
	Diff  func(*userv1.Identity) string
}

func matchIdentityWaitCriterion(actual *userv1.Identity, criteria ...IdentityWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

// WaitForIdentity waits until there is an Identity with the given name available
func (a *MemberAwaitility) WaitForIdentity(t *testing.T, name string, criteria ...IdentityWaitCriterion) (*userv1.Identity, error) {
	t.Logf("waiting for Identity '%s'", name)
	identity := &userv1.Identity{}
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		identity = &userv1.Identity{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name}, identity); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		if !matchIdentityWaitCriterion(identity, criteria...) {
			return false, nil
		}
		if identity.Name != "" && identity.User.Name != "" {
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		a.printIdentities(t, name)
	}
	return identity, err
}

func (a *MemberAwaitility) printIdentities(t *testing.T, expectedName string) {
	buf := &strings.Builder{}
	buf.WriteString(fmt.Sprintf("failed to find Identity '%s'\n", expectedName))
	buf.WriteString(a.listAndReturnContent("Identity", "", &userv1.IdentityList{}))
	t.Log(buf.String())
}

// UntilIdentityHasLabel checks if the Identity has the expected label
func UntilIdentityHasLabel(key, value string) IdentityWaitCriterion {
	return IdentityWaitCriterion{
		Match: func(actual *userv1.Identity) bool {
			return actual.Labels[key] == value
		},
		Diff: func(actual *userv1.Identity) string {
			return fmt.Sprintf("expected Identity label '%s' to be '%s'\nbut it was '%s'", key, value, actual.Labels[key])
		},
	}
}

// WaitUntilUserAccountDeleted waits until the UserAccount with the given name is not found
func (a *MemberAwaitility) WaitUntilUserAccountDeleted(t *testing.T, name string) error {
	t.Logf("waiting until UserAccount '%s' in namespace '%s' is deleted", name, a.Namespace)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		ua := &toolchainv1alpha1.UserAccount{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: a.Namespace, Name: name}, ua); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	})
}

// WaitUntilUserDeleted waits until the User with the given name is not found
func (a *MemberAwaitility) WaitUntilUserDeleted(t *testing.T, name string) error {
	t.Logf("waiting until User is deleted '%s'", name)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		user := &userv1.User{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name}, user); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		if _, exists := user.Labels[toolchainv1alpha1.OwnerLabelKey]; exists {
			return false, nil
		}
		return true, nil
	})
}

// WaitUntilIdentityDeleted waits until the Identity with the given name is not found
func (a *MemberAwaitility) WaitUntilIdentityDeleted(t *testing.T, name string) error {
	t.Logf("waiting until Identity is deleted '%s'", name)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		identity := &userv1.Identity{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Name: name}, identity); err != nil {
			if errors.IsNotFound(err) {
				return true, nil
			}
			return false, err
		}
		if _, exists := identity.Labels[toolchainv1alpha1.OwnerLabelKey]; exists {
			return false, nil
		}
		return true, nil
	})
}

// GetConsoleURL retrieves Web Console Route and returns its URL
func (a *MemberAwaitility) GetConsoleURL(t *testing.T) string {
	route := &routev1.Route{}
	namespacedName := types.NamespacedName{Namespace: "openshift-console", Name: "console"}
	err := a.Client.Get(context.TODO(), namespacedName, route)
	require.NoError(t, err)
	return fmt.Sprintf("https://%s/%s", route.Spec.Host, route.Spec.Path)
}

// WaitUntilClusterResourceQuotasDeleted waits until all ClusterResourceQuotas with the given owner label are deleted (ie, none is found)
func (a *MemberAwaitility) WaitUntilClusterResourceQuotasDeleted(t *testing.T, username string) error {
	t.Logf("waiting for deletion of ClusterResourceQuotas for user '%s'", username)
	return wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		labels := map[string]string{
			toolchainv1alpha1.SpaceLabelKey: username,
		}
		opts := client.MatchingLabels(labels)
		quotaList := &quotav1.ClusterResourceQuotaList{}
		if err := a.Client.List(context.TODO(), quotaList, opts); err != nil {
			return false, err
		}
		if len(quotaList.Items) == 0 {
			return true, nil
		}
		return false, nil
	})
}

// MemberStatusWaitCriterion a struct to compare with a given MemberStatus
type MemberStatusWaitCriterion struct {
	Match func(*toolchainv1alpha1.MemberStatus) bool
	Diff  func(*toolchainv1alpha1.MemberStatus) string
}

func matchMemberStatusWaitCriterion(actual *toolchainv1alpha1.MemberStatus, criteria ...MemberStatusWaitCriterion) bool {
	for _, c := range criteria {
		if !c.Match(actual) {
			return false
		}
	}
	return true
}

func (a *MemberAwaitility) printMemberStatusWaitCriterionDiffs(t *testing.T, actual *toolchainv1alpha1.MemberStatus, criteria ...MemberStatusWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find MemberStatus\n")
		buf.WriteString(a.listAndReturnContent("MemberStatus", "", &toolchainv1alpha1.MemberStatusList{}))
		buf.WriteString(a.listAndReturnContent("ToolchainCluster", "", &toolchainv1alpha1.ToolchainClusterList{}))
	} else {
		buf.WriteString("failed to find MemberStatus with matching criteria:\n")
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
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
	t.Log(buf.String())
}

// UntilMemberStatusHasConditions returns a `MemberStatusWaitCriterion` which checks that the given
// MemberStatus has exactly all the given status conditions
func UntilMemberStatusHasConditions(expected ...toolchainv1alpha1.Condition) MemberStatusWaitCriterion {
	return MemberStatusWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MemberStatus) bool {
			return test.ConditionsMatch(actual.Status.Conditions, expected...)
		},
		Diff: func(actual *toolchainv1alpha1.MemberStatus) string {
			return fmt.Sprintf("expected conditions to match:\n%s", Diff(expected, actual.Status.Conditions))
		},
	}
}

// UntilMemberStatusHasUsageSet returns a `MemberStatusWaitCriterion` which checks that the given
// MemberStatus has some non-zero resource usage set
func UntilMemberStatusHasUsageSet() MemberStatusWaitCriterion {
	return MemberStatusWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MemberStatus) bool {
			return hasMemberStatusUsageSet(actual.Status)
		},
		Diff: func(actual *toolchainv1alpha1.MemberStatus) string {
			return fmt.Sprintf("expected MemberStatus to have 'master' and/or 'worker' usages set: %v", actual.Status.ResourceUsage.MemoryUsagePerNodeRole)
		},
	}
}

func hasMemberStatusUsageSet(status toolchainv1alpha1.MemberStatusStatus) bool {
	usage := status.ResourceUsage.MemoryUsagePerNodeRole
	// Usage of nodes can be for both master and worker types.
	// We check explicitly that at least the one for `worker` node type is present,
	// since the `master` node type may not be available on hypershift clusters for example.
	return len(usage) >= 1 && usage["worker"] > 0
}

// UntilMemberStatusHasConsoleURLSet returns a `MemberStatusWaitCriterion` which checks that the given
// MemberStatus has a non-empty console url set
func UntilMemberStatusHasConsoleURLSet(expectedURL string, expectedCondition toolchainv1alpha1.Condition) MemberStatusWaitCriterion {
	return MemberStatusWaitCriterion{
		Match: func(actual *toolchainv1alpha1.MemberStatus) bool {
			return actual.Status.Routes != nil &&
				actual.Status.Routes.ConsoleURL == expectedURL &&
				test.ConditionsMatch(actual.Status.Routes.Conditions, expectedCondition)
		},
		Diff: func(actual *toolchainv1alpha1.MemberStatus) string {
			e, _ := yaml.Marshal(expectedCondition)
			a, _ := yaml.Marshal(actual.Status.Routes)
			return fmt.Sprintf("expected MemberStatus route for Console to be '%s' with condition\n%s\nbut it was: \n%s", expectedURL, e, a)
		},
	}
}

// WaitForMemberStatus waits until the MemberStatus is available with the provided criteria, if any
func (a *MemberAwaitility) WaitForMemberStatus(t *testing.T, criteria ...MemberStatusWaitCriterion) error {
	name := "toolchain-member-status"
	t.Logf("waiting for MemberStatus '%s' to match criteria", name)
	// there should only be one member status with the name toolchain-member-status
	var memberStatus *toolchainv1alpha1.MemberStatus
	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		// retrieve the memberstatus from the member namespace
		obj := &toolchainv1alpha1.MemberStatus{}
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
		memberStatus = obj
		return matchMemberStatusWaitCriterion(obj, criteria...), nil
	})
	if err != nil {
		a.printMemberStatusWaitCriterionDiffs(t, memberStatus, criteria...)
	}
	return err
}

// GetMemberOperatorConfig returns MemberOperatorConfig instance, nil if not found
func (a *MemberAwaitility) GetMemberOperatorConfig(t *testing.T) *toolchainv1alpha1.MemberOperatorConfig {
	config := &toolchainv1alpha1.MemberOperatorConfig{}
	if err := a.Client.Get(context.TODO(), test.NamespacedName(a.Namespace, "config"), config); err != nil {
		if errors.IsNotFound(err) {
			return nil
		}
		require.NoError(t, err)
	}
	return config
}

// MemberOperatorConfigWaitCriterion a function to check that an MemberOperatorConfig has the expected criteria
type MemberOperatorConfigWaitCriterion func(*HostAwaitility, *MemberAwaitility, *toolchainv1alpha1.MemberOperatorConfig) bool

// UntilMemberConfigMatches returns a `MemberOperatorConfigWaitCriterion` which checks that the given
// MemberOperatorConfig matches the provided one
func UntilMemberConfigMatches(expectedMemberOperatorConfigSpec toolchainv1alpha1.MemberOperatorConfigSpec) MemberOperatorConfigWaitCriterion {
	return func(h *HostAwaitility, a *MemberAwaitility, memberConfig *toolchainv1alpha1.MemberOperatorConfig) bool {
		return reflect.DeepEqual(expectedMemberOperatorConfigSpec, memberConfig.Spec)
	}
}

// WaitForMemberOperatorConfig waits until the MemberOperatorConfig is available with the provided criteria, if any
func (a *MemberAwaitility) WaitForMemberOperatorConfig(t *testing.T, hostAwait *HostAwaitility, criteria ...MemberOperatorConfigWaitCriterion) (*toolchainv1alpha1.MemberOperatorConfig, error) {
	// there should only be one MemberOperatorConfig with the name config
	name := "config"
	t.Logf("waiting for MemberOperatorConfig '%s'", name)
	memberOperatorConfig := &toolchainv1alpha1.MemberOperatorConfig{}
	err := wait.Poll(a.RetryInterval, 2*a.Timeout, func() (done bool, err error) {
		memberOperatorConfig = &toolchainv1alpha1.MemberOperatorConfig{}
		// retrieve the MemberOperatorConfig from the member namespace
		err = a.Client.Get(context.TODO(),
			types.NamespacedName{
				Namespace: a.Namespace,
				Name:      name,
			},
			memberOperatorConfig)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		for _, match := range criteria {
			if !match(hostAwait, a, memberOperatorConfig) {
				return false, nil
			}
		}
		return true, nil
	})
	return memberOperatorConfig, err
}

// GetMemberOperatorPod returns the pod running the member operator controllers
func (a *MemberAwaitility) GetMemberOperatorPod() (corev1.Pod, error) {
	pods := corev1.PodList{}
	if err := a.Client.List(context.TODO(), &pods, client.InNamespace(a.Namespace), client.MatchingLabels{"control-plane": "controller-manager"}); err != nil {
		return corev1.Pod{}, err
	}
	if len(pods.Items) != 1 {
		return corev1.Pod{}, fmt.Errorf("unexpected number of pods with label 'control-plane=controller-manager' in namespace '%s': %d ", a.Namespace, len(pods.Items))
	}
	return pods.Items[0], nil
}

func (a *MemberAwaitility) WaitForMemberWebhooks(t *testing.T, image string) {
	a.waitForUsersPodPriorityClass(t)
	a.waitForService(t)
	a.waitForWebhookDeployment(t, image)
	ca := a.verifySecret(t)
	a.verifyMutatingWebhookConfig(t, ca)
	a.verifyValidatingWebhookConfig(t, ca)
}

func (a *MemberAwaitility) waitForUsersPodPriorityClass(t *testing.T) {
	t.Logf("checking PrioritiyClass resource '%s'", "sandbox-users-pods")
	actualPrioClass := &schedulingv1.PriorityClass{}
	a.waitForResource(t, "", "sandbox-users-pods", actualPrioClass)

	assert.Equal(t, codereadyToolchainProviderLabel, actualPrioClass.Labels)
	assert.Equal(t, int32(-3), actualPrioClass.Value)
	assert.False(t, actualPrioClass.GlobalDefault)
	assert.Equal(t, "Priority class for pods in users' namespaces", actualPrioClass.Description)
}

func (a *MemberAwaitility) waitForResource(t *testing.T, namespace, name string, object client.Object) {
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		if err := a.Client.Get(context.TODO(), test.NamespacedName(namespace, name), object); err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	})
	require.NoError(t, err)
}

func (a *MemberAwaitility) waitForService(t *testing.T) {
	t.Logf("waiting for Service '%s' in namespace '%s'", "member-operator-webhook", a.Namespace)
	actualService := &corev1.Service{}
	a.waitForResource(t, a.Namespace, "member-operator-webhook", actualService)
	assert.Equal(t, map[string]string{
		"app":                                  "member-operator-webhook",
		"toolchain.dev.openshift.com/provider": "codeready-toolchain",
	}, actualService.Labels)
	require.Len(t, actualService.Spec.Ports, 1)
	assert.Equal(t, int32(443), actualService.Spec.Ports[0].Port)
	assert.Equal(t, intstr.IntOrString{
		IntVal: 8443,
	}, actualService.Spec.Ports[0].TargetPort)
	assert.Equal(t, appMemberOperatorWebhookLabel, actualService.Spec.Selector)
}

func (a *MemberAwaitility) waitForWebhookDeployment(t *testing.T, image string) {
	t.Logf("checking Deployment '%s' in namespace '%s'", "member-operator-webhook", a.Namespace)
	actualDeployment := a.WaitForDeploymentToGetReady(t, "member-operator-webhook", 1,
		DeploymentHasContainerWithImage("mutator", image))

	assert.Equal(t, bothWebhookLabels, actualDeployment.Labels)
	assert.Equal(t, int32(1), *actualDeployment.Spec.Replicas)
	assert.Equal(t, appMemberOperatorWebhookLabel, actualDeployment.Spec.Selector.MatchLabels)

	template := actualDeployment.Spec.Template
	assert.Equal(t, "member-operator-webhook", template.ObjectMeta.Name)
	assert.Equal(t, appMemberOperatorWebhookLabel, template.ObjectMeta.Labels)
	require.Len(t, template.Spec.Volumes, 1)
	assert.Equal(t, "webhook-certs", template.Spec.Volumes[0].Name)
	assert.Equal(t, "webhook-certs", template.Spec.Volumes[0].Secret.SecretName)
	require.Len(t, template.Spec.Containers, 1)

	container := template.Spec.Containers[0]
	assert.Equal(t, "mutator", container.Name)
	assert.NotEmpty(t, container.Image)
	assert.Equal(t, []string{"member-operator-webhook"}, container.Command)
	assert.Equal(t, corev1.PullIfNotPresent, container.ImagePullPolicy)
	assert.NotEmpty(t, container.Resources)

	assert.Len(t, container.VolumeMounts, 1)
	assert.Equal(t, "webhook-certs", container.VolumeMounts[0].Name)
	assert.Equal(t, "/etc/webhook/certs", container.VolumeMounts[0].MountPath)
	assert.True(t, container.VolumeMounts[0].ReadOnly)

	a.WaitForDeploymentToGetReady(t, "member-operator-webhook", 1)
}

func (a *MemberAwaitility) verifySecret(t *testing.T) []byte {
	t.Logf("checking Secret '%s' in namespace '%s'", "webhook-certs", a.Namespace)
	secret := &corev1.Secret{}
	a.waitForResource(t, a.Namespace, "webhook-certs", secret)
	assert.NotEmpty(t, secret.Data["server-key.pem"])
	assert.NotEmpty(t, secret.Data["server-cert.pem"])
	ca := secret.Data["ca-cert.pem"]
	assert.NotEmpty(t, ca)
	return ca
}

func (a *MemberAwaitility) verifyMutatingWebhookConfig(t *testing.T, ca []byte) {
	t.Logf("checking MutatingWebhookConfiguration")
	actualMutWbhConf := &admv1.MutatingWebhookConfiguration{}
	a.waitForResource(t, "", "member-operator-webhook", actualMutWbhConf)
	assert.Equal(t, bothWebhookLabels, actualMutWbhConf.Labels)
	require.Len(t, actualMutWbhConf.Webhooks, 2)

	type Rule struct {
		Operations  []admv1.OperationType
		APIGroups   []string
		APIVersions []string
		Resources   []string
	}

	tests := map[string]struct {
		Index         int
		Name          string
		Path          string
		FailurePolicy admv1.FailurePolicyType
		Rule          Rule
	}{
		"users pods webhook": {
			Index:         0,
			Name:          "users.pods.webhook.sandbox",
			Path:          "/mutate-users-pods",
			FailurePolicy: admv1.Ignore,
			Rule: Rule{
				Operations:  []admv1.OperationType{"CREATE"},
				APIGroups:   []string{""},
				APIVersions: []string{"v1"},
				Resources:   []string{"pods"},
			},
		},
		"virtual machine webhook": {
			Index:         1,
			Name:          "users.virtualmachines.webhook.sandbox",
			Path:          "/mutate-virtual-machines",
			FailurePolicy: admv1.Fail,
			Rule: Rule{
				Operations:  []admv1.OperationType{"CREATE"},
				APIGroups:   []string{"kubevirt.io"},
				APIVersions: []string{"v1"},
				Resources:   []string{"virtualmachines"},
			},
		},
	}
	for k, tc := range tests {
		t.Run(k, func(t *testing.T) {
			webhook := actualMutWbhConf.Webhooks[tc.Index]
			assert.Equal(t, tc.Name, webhook.Name)
			assert.Equal(t, []string{"v1"}, webhook.AdmissionReviewVersions)
			assert.Equal(t, admv1.SideEffectClassNone, *webhook.SideEffects)
			assert.Equal(t, int32(5), *webhook.TimeoutSeconds)
			assert.Equal(t, admv1.NeverReinvocationPolicy, *webhook.ReinvocationPolicy)
			assert.Equal(t, tc.FailurePolicy, *webhook.FailurePolicy)
			assert.Equal(t, admv1.Equivalent, *webhook.MatchPolicy)
			assert.Equal(t, codereadyToolchainProviderLabel, webhook.NamespaceSelector.MatchLabels)
			assert.Equal(t, ca, webhook.ClientConfig.CABundle)
			assert.Equal(t, "member-operator-webhook", webhook.ClientConfig.Service.Name)
			assert.Equal(t, a.Namespace, webhook.ClientConfig.Service.Namespace)
			assert.Equal(t, tc.Path, *webhook.ClientConfig.Service.Path)
			assert.Equal(t, int32(443), *webhook.ClientConfig.Service.Port)
			require.Len(t, webhook.Rules, 1)

			rule := webhook.Rules[0]
			assert.Equal(t, tc.Rule.Operations, rule.Operations)
			assert.Equal(t, tc.Rule.APIGroups, rule.APIGroups)
			assert.Equal(t, tc.Rule.APIVersions, rule.APIVersions)
			assert.Equal(t, tc.Rule.Resources, rule.Resources)
			assert.Equal(t, admv1.NamespacedScope, *rule.Scope)
		})
	}
}

func (a *MemberAwaitility) verifyValidatingWebhookConfig(t *testing.T, ca []byte) {
	t.Logf("checking ValidatingWebhookConfiguration '%s'", "member-operator-validating-webhook")
	actualValWbhConf := &admv1.ValidatingWebhookConfiguration{}
	a.waitForResource(t, "", "member-operator-validating-webhook", actualValWbhConf)
	assert.Equal(t, bothWebhookLabels, actualValWbhConf.Labels)
	// require.Len(t, actualValWbhConf.Webhooks, 2)

	rolebindingWebhook := actualValWbhConf.Webhooks[0]
	assert.Equal(t, "users.rolebindings.webhook.sandbox", rolebindingWebhook.Name)
	assert.Equal(t, []string{"v1"}, rolebindingWebhook.AdmissionReviewVersions)
	assert.Equal(t, admv1.SideEffectClassNone, *rolebindingWebhook.SideEffects)
	assert.Equal(t, int32(5), *rolebindingWebhook.TimeoutSeconds)
	assert.Equal(t, admv1.Ignore, *rolebindingWebhook.FailurePolicy)
	assert.Equal(t, admv1.Equivalent, *rolebindingWebhook.MatchPolicy)
	assert.Equal(t, codereadyToolchainProviderLabel, rolebindingWebhook.NamespaceSelector.MatchLabels)
	assert.Equal(t, ca, rolebindingWebhook.ClientConfig.CABundle)
	assert.Equal(t, "member-operator-webhook", rolebindingWebhook.ClientConfig.Service.Name)
	assert.Equal(t, a.Namespace, rolebindingWebhook.ClientConfig.Service.Namespace)
	assert.Equal(t, "/validate-users-rolebindings", *rolebindingWebhook.ClientConfig.Service.Path)
	assert.Equal(t, int32(443), *rolebindingWebhook.ClientConfig.Service.Port)
	require.Len(t, rolebindingWebhook.Rules, 1)

	rolebindingRule := rolebindingWebhook.Rules[0]
	assert.Equal(t, []admv1.OperationType{admv1.Create, admv1.Update}, rolebindingRule.Operations)
	assert.Equal(t, []string{"rbac.authorization.k8s.io", "authorization.openshift.io"}, rolebindingRule.APIGroups)
	assert.Equal(t, []string{"v1"}, rolebindingRule.APIVersions)
	assert.Equal(t, []string{"rolebindings"}, rolebindingRule.Resources)
	assert.Equal(t, admv1.NamespacedScope, *rolebindingRule.Scope)

	k8sImagePullerWebhook := actualValWbhConf.Webhooks[1]
	assert.Equal(t, "users.kubernetesimagepullers.webhook.sandbox", k8sImagePullerWebhook.Name)
	assert.Equal(t, []string{"v1"}, k8sImagePullerWebhook.AdmissionReviewVersions)
	assert.Equal(t, admv1.SideEffectClassNone, *k8sImagePullerWebhook.SideEffects)
	assert.Equal(t, int32(5), *k8sImagePullerWebhook.TimeoutSeconds)
	assert.Equal(t, admv1.Fail, *k8sImagePullerWebhook.FailurePolicy)
	assert.Equal(t, admv1.Equivalent, *k8sImagePullerWebhook.MatchPolicy)
	assert.Equal(t, codereadyToolchainProviderLabel, k8sImagePullerWebhook.NamespaceSelector.MatchLabels)
	assert.Equal(t, ca, k8sImagePullerWebhook.ClientConfig.CABundle)
	assert.Equal(t, "member-operator-webhook", k8sImagePullerWebhook.ClientConfig.Service.Name)
	assert.Equal(t, a.Namespace, k8sImagePullerWebhook.ClientConfig.Service.Namespace)
	assert.Equal(t, "/validate-users-kubernetesimagepullers", *k8sImagePullerWebhook.ClientConfig.Service.Path)
	assert.Equal(t, int32(443), *k8sImagePullerWebhook.ClientConfig.Service.Port)
	require.Len(t, k8sImagePullerWebhook.Rules, 1)

	k8sImagePullerRule := k8sImagePullerWebhook.Rules[0]
	assert.Equal(t, []admv1.OperationType{admv1.Create}, k8sImagePullerRule.Operations)
	assert.Equal(t, []string{"che.eclipse.org"}, k8sImagePullerRule.APIGroups)
	assert.Equal(t, []string{"v1alpha1"}, k8sImagePullerRule.APIVersions)
	assert.Equal(t, []string{"kubernetesimagepullers"}, k8sImagePullerRule.Resources)
	assert.Equal(t, admv1.NamespacedScope, *k8sImagePullerRule.Scope)

	spacebindingrequestWebhook := actualValWbhConf.Webhooks[2]
	assert.Equal(t, "users.spacebindingrequests.webhook.sandbox", spacebindingrequestWebhook.Name)
	assert.Equal(t, []string{"v1"}, spacebindingrequestWebhook.AdmissionReviewVersions)
	assert.Equal(t, admv1.SideEffectClassNone, *spacebindingrequestWebhook.SideEffects)
	assert.Equal(t, int32(5), *spacebindingrequestWebhook.TimeoutSeconds)
	assert.Equal(t, admv1.Fail, *spacebindingrequestWebhook.FailurePolicy)
	assert.Equal(t, admv1.Equivalent, *spacebindingrequestWebhook.MatchPolicy)
	assert.Equal(t, codereadyToolchainProviderLabel, spacebindingrequestWebhook.NamespaceSelector.MatchLabels)
	assert.Equal(t, ca, spacebindingrequestWebhook.ClientConfig.CABundle)
	assert.Equal(t, "member-operator-webhook", spacebindingrequestWebhook.ClientConfig.Service.Name)
	assert.Equal(t, a.Namespace, spacebindingrequestWebhook.ClientConfig.Service.Namespace)
	assert.Equal(t, "/validate-spacebindingrequests", *spacebindingrequestWebhook.ClientConfig.Service.Path)
	assert.Equal(t, int32(443), *spacebindingrequestWebhook.ClientConfig.Service.Port)
	require.Len(t, spacebindingrequestWebhook.Rules, 1)

	spacebindingrequestRule := spacebindingrequestWebhook.Rules[0]
	assert.Equal(t, []admv1.OperationType{admv1.Create, admv1.Update}, spacebindingrequestRule.Operations)
	assert.Equal(t, []string{"toolchain.dev.openshift.com"}, spacebindingrequestRule.APIGroups)
	assert.Equal(t, []string{"v1alpha1"}, spacebindingrequestRule.APIVersions)
	assert.Equal(t, []string{"spacebindingrequests"}, spacebindingrequestRule.Resources)
	assert.Equal(t, admv1.NamespacedScope, *spacebindingrequestRule.Scope)

}

func (a *MemberAwaitility) WaitForAutoscalingBufferApp(t *testing.T) {
	a.verifyAutoscalingBufferPriorityClass(t)
	a.verifyAutoscalingBufferDeployment(t)
}

func (a *MemberAwaitility) verifyAutoscalingBufferPriorityClass(t *testing.T) {
	t.Logf("checking PrioritiyClass '%s'", "member-operator-autoscaling-buffer")
	actualPrioClass := &schedulingv1.PriorityClass{}
	a.waitForResource(t, "", "member-operator-autoscaling-buffer", actualPrioClass)

	assert.Equal(t, codereadyToolchainProviderLabel, actualPrioClass.Labels)
	assert.Equal(t, int32(-5), actualPrioClass.Value)
	assert.False(t, actualPrioClass.GlobalDefault)
	assert.Equal(t, "This priority class is to be used by the autoscaling buffer pod only", actualPrioClass.Description)
}

func (a *MemberAwaitility) verifyAutoscalingBufferDeployment(t *testing.T) {
	t.Logf("checking Deployment '%s' in namespace '%s'", "autoscaling-buffer", a.Namespace)
	actualDeployment := &appsv1.Deployment{}
	a.waitForResource(t, a.Namespace, "autoscaling-buffer", actualDeployment)

	assert.Equal(t, map[string]string{
		"app":                                  "autoscaling-buffer",
		"toolchain.dev.openshift.com/provider": "codeready-toolchain",
	}, actualDeployment.Labels)
	assert.Equal(t, int32(2), *actualDeployment.Spec.Replicas)
	assert.Equal(t, map[string]string{"app": "autoscaling-buffer"}, actualDeployment.Spec.Selector.MatchLabels)

	template := actualDeployment.Spec.Template
	assert.Equal(t, map[string]string{"app": "autoscaling-buffer"}, template.ObjectMeta.Labels)

	assert.Equal(t, "member-operator-autoscaling-buffer", template.Spec.PriorityClassName)
	assert.Equal(t, int64(0), *template.Spec.TerminationGracePeriodSeconds)

	require.Len(t, template.Spec.Containers, 1)
	container := template.Spec.Containers[0]
	assert.Equal(t, "autoscaling-buffer", container.Name)
	assert.Equal(t, "gcr.io/google_containers/pause-amd64:3.2", container.Image)
	assert.Equal(t, corev1.PullIfNotPresent, container.ImagePullPolicy)

	expectedMemory, err := resource.ParseQuantity("50Mi")
	require.NoError(t, err)
	assert.True(t, container.Resources.Requests.Memory().Equal(expectedMemory))
	assert.True(t, container.Resources.Limits.Memory().Equal(expectedMemory))

	a.WaitForDeploymentToGetReady(t, "autoscaling-buffer", 2)
}

// WaitForExpectedNumberOfResources waits until the number of resources matches the expected count
func (a *MemberAwaitility) WaitForExpectedNumberOfResources(t *testing.T, namespace, kind string, expected int, list func() (int, error)) error {
	if actual, err := a.waitForExpectedNumberOfResources(expected, list); err != nil {
		t.Logf("expected number of resources of kind '%s' in namespace '%s' to be %d but it was %d", kind, namespace, expected, actual)
		return err
	}
	return nil
}

// WaitForExpectedNumberOfClusterResources waits until the number of resources matches the expected count
func (a *MemberAwaitility) WaitForExpectedNumberOfClusterResources(t *testing.T, kind string, expected int, list func() (int, error)) error {
	if actual, err := a.waitForExpectedNumberOfResources(expected, list); err != nil {
		t.Logf("expected number of resources of kind '%s' to be %d but it was %d", kind, expected, actual)
		return err
	}
	return nil
}

func (a *MemberAwaitility) waitForExpectedNumberOfResources(expected int, list func() (int, error)) (int, error) {
	var actual int
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		a, err := list()
		if err != nil {
			return false, err
		}
		actual = a
		return actual == expected, nil
	})
	return actual, err
}

func (a *MemberAwaitility) UpdatePod(t *testing.T, namespace, podName string, modifyPod func(pod *corev1.Pod)) (*corev1.Pod, error) {
	var m *corev1.Pod
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		freshPod := &corev1.Pod{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{Namespace: namespace, Name: podName}, freshPod); err != nil {
			return true, err
		}

		modifyPod(freshPod)
		if err := a.Client.Update(context.TODO(), freshPod); err != nil {
			t.Logf("error updating Pod '%s' Will retry again...", podName)
			return false, nil // nolint:nilerr
		}
		m = freshPod
		return true, nil
	})
	return m, err
}

func (a *MemberAwaitility) UpdateConfigMap(t *testing.T, namespace, cmName string, modifyCM func(*corev1.ConfigMap)) (*corev1.ConfigMap, error) {
	var cm *corev1.ConfigMap
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &corev1.ConfigMap{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{
			Namespace: namespace,
			Name:      cmName},
			obj); err != nil {
			return true, err
		}
		modifyCM(obj)
		if err := a.Client.Update(context.TODO(), obj); err != nil {
			t.Logf("error updating ConfigMap '%s' Will retry again...", cmName)
			return false, nil // nolint:nilerr
		}
		cm = obj
		return true, nil
	})
	return cm, err
}

func (a *MemberAwaitility) WaitForEnvironment(t *testing.T, namespace, name string, criteria ...LabelWaitCriterion) (*appstudiov1.Environment, error) {
	t.Logf("waiting for Environment resource '%s' to exist in namespace '%s'", name, namespace)
	var env *appstudiov1.Environment
	err := wait.Poll(a.RetryInterval, a.Timeout, func() (done bool, err error) {
		obj := &appstudiov1.Environment{}
		if err := a.Client.Get(context.TODO(), types.NamespacedName{
			Namespace: namespace,
			Name:      name},
			obj); errors.IsNotFound(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		env = obj
		return matchLabelWaitCriteria(obj.ObjectMeta, criteria...), nil
	})
	if err != nil {
		t.Logf("failed to wait for Environment")
		a.printEnvironmentWaitCriterionDiffs(t, env, criteria...)
		return nil, err
	}
	t.Logf("found Environment %s with custom labels", env.Name)
	return env, err
}

func (a *MemberAwaitility) printEnvironmentWaitCriterionDiffs(t *testing.T, actual *appstudiov1.Environment, criteria ...LabelWaitCriterion) {
	buf := &strings.Builder{}
	if actual == nil {
		buf.WriteString("failed to find Environment\n")
		buf.WriteString(a.listAndReturnContent("Environment", "", &appstudiov1.EnvironmentList{}))
	} else {
		buf.WriteString("failed to find Environment with matching criteria:\n")
		buf.WriteString("----\n")
		buf.WriteString("actual:\n")
		y, _ := StringifyObject(actual)
		buf.Write(y)
		buf.WriteString("\n----\n")
		buf.WriteString("diffs:\n")
		for _, c := range criteria {
			if !c.Match(actual.ObjectMeta) {
				buf.WriteString(c.Diff(actual.ObjectMeta))
				buf.WriteString("\n")
			}
		}
	}
	t.Log(buf.String())
}

func (a *MemberAwaitility) GetContainerEnv(t *testing.T, name string) string {
	deployment := a.WaitForDeploymentToGetReady(t, "member-operator-controller-manager", 1)
	var value string
containers:
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == "manager" {
			for _, env := range container.Env {
				if env.Name == name {
					value = env.Value
					break containers
				}
			}
		}
	}
	return value
}
