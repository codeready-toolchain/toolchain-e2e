package parallel

import (
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	testcommonspace "github.com/codeready-toolchain/toolchain-common/pkg/test/space"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/tiers"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	"github.com/redhat-cop/operator-utils/pkg/util"
	v1 "k8s.io/api/core/v1"

	"github.com/stretchr/testify/require"
)

type TestCase struct {
	username                  string
	email                     string
	expectedCompliantUsername string
}

func TestTransformUsernameWithSpaceConflict(t *testing.T) {
	t.Parallel()

	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	// given
	conflictingSpace, _, _ := CreateSpace(t, awaitilities, testcommonspace.WithName("conflicting"))

	// when
	user := NewSignupRequest(awaitilities).
		Username(conflictingSpace.Name).
		TargetCluster(memberAwait).
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)
	userSignup := user.UserSignup

	// then
	expectedCompliantUsername := userSignup.Status.CompliantUsername
	require.Equal(t, expectedCompliantUsername, fmt.Sprintf("%s-2", conflictingSpace.Name))

	t.Run("when signup is deactivated, Space is stuck in terminating state, and when it reactivates then it should generate a new name", func(t *testing.T) {
		// given
		// let's get a namespace of the space
		namespaceName := user.Space.Status.ProvisionedNamespaces[0].Name
		// and add a dummy finalizer there so it will get stuck
		_, err := wait.For(t, memberAwait.Awaitility, &v1.Namespace{}).
			Update(namespaceName, memberAwait.Namespace, func(ns *v1.Namespace) {
				util.AddFinalizer(ns, "test/finalizer.toolchain.e2e.tests")
			})
		require.NoError(t, err)

		// don't forget to clean the finalizer up
		defer func() {
			t.Log("cleaning up the finalizer")
			_, err = wait.For(t, memberAwait.Awaitility, &v1.Namespace{}).
				Update(namespaceName, memberAwait.Namespace, func(ns *v1.Namespace) {
					util.RemoveFinalizer(ns, "test/finalizer.toolchain.e2e.tests")
				})
			require.NoError(t, err)
		}()

		// now deactivate the usersignup
		_, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
			Update(userSignup.Name, hostAwait.Namespace, func(us *toolchainv1alpha1.UserSignup) {
				states.SetDeactivated(us, true)
			})
		require.NoError(t, err)

		// wait until it is deactivated, SpaceBinding is gone, and Space is in terminating state
		_, err = hostAwait.WaitForUserSignup(t, userSignup.Name,
			wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
		require.NoError(t, err)
		err = hostAwait.WaitUntilSpaceBindingsWithLabelDeleted(t, toolchainv1alpha1.SpaceBindingMasterUserRecordLabelKey, expectedCompliantUsername)
		require.NoError(t, err)
		_, err = hostAwait.WaitForSpace(t, expectedCompliantUsername, wait.UntilSpaceIsBeingDeleted())
		require.NoError(t, err)

		// when
		userSignup, err = wait.For(t, hostAwait.Awaitility, &toolchainv1alpha1.UserSignup{}).
			Update(userSignup.Name, hostAwait.Namespace, func(us *toolchainv1alpha1.UserSignup) {
				states.SetApprovedManually(us, true)
			})
		require.NoError(t, err)

		// then
		userSignup, _ = VerifyUserRelatedResources(t, awaitilities, userSignup, "deactivate30", ExpectAnyUserAccount())
		VerifySpaceRelatedResources(t, awaitilities, userSignup, tiers.GetDefaultSpaceTierName(t, hostAwait))
		VerifyResourcesProvisionedForSignup(t, awaitilities, userSignup)
		require.Equal(t, fmt.Sprintf("%s-3", conflictingSpace.Name), userSignup.Status.CompliantUsername)
	})
}

func TestTransformUsername(t *testing.T) {
	t.Parallel()

	awaitilities := WaitForDeployments(t)
	testCases := []TestCase{
		// Create UserSignup with a username that we don't need to transform
		{
			username:                  "paul-no-transform",
			email:                     "paulnotransform@hotel.com",
			expectedCompliantUsername: "paul-no-transform",
		},
		// Create UserSignup with a username to transform
		{
			username:                  "paul@hotel.com",
			email:                     "paul@hotel.com",
			expectedCompliantUsername: "paul",
		},
		// Create another UserSignup with the original username matching the transformed username of the existing signup
		{
			username:                  "paul",
			email:                     "paulathotel@hotel.com",
			expectedCompliantUsername: "paul-2",
		},
		// Create a usersignup where the length of the username is greater than 20 chars, and is transformed by truncating
		{
			username:                  "username-greater-than-20",
			email:                     "paulathotel@hotel.com",
			expectedCompliantUsername: "username-greater-tha",
		},
		// Create a usersignup where the name is greater than 20 chars, but when truncating the username it has a forbidden suffix. Check the compliant username is replacing the suffix, instead of adding
		{
			username:                  "username-with-admin-more-than-20-chars",
			email:                     "paulathotel@hotel.com",
			expectedCompliantUsername: "username-with-ad-crt",
		},
	}

	for _, testCase := range testCases {
		assertComplaintUsernameForNewSignup(t, awaitilities, testCase)
	}

	testCasesPrefix := []TestCase{
		// prefix with hyphen
		{
			username:                  "-paul",
			email:                     "paul@hotel.com",
			expectedCompliantUsername: "crt-%s-paul",
		},
		// prefix without delimiter
		{
			username:                  "paul",
			email:                     "paul@hotel.com",
			expectedCompliantUsername: "crt-%spaul",
		},
		// prefix as a name
		{
			username:                  "",
			email:                     "paul@hotel.com",
			expectedCompliantUsername: "crt-%s",
		},
	}

	// Create another UserSignups with a forbidden prefix
	for _, prefix := range []string{"kube", "openshift", "default", "redhat", "sandbox"} {
		// prefix with hyphen
		for _, testCase := range testCasesPrefix {
			assertComplaintUsernameForNewSignup(t, awaitilities, TestCase{
				username:                  prefix + testCase.username,
				email:                     testCase.email,
				expectedCompliantUsername: fmt.Sprintf(testCase.expectedCompliantUsername, prefix),
			})
		}
	}

	testCasesSuffix := []TestCase{
		// suffix with hyphen
		{
			username:                  "paul-",
			email:                     "paul@hotel.com",
			expectedCompliantUsername: "paul-%s-crt",
		},
		// suffix without delimiter
		{
			username:                  "paul",
			email:                     "paul@hotel.com",
			expectedCompliantUsername: "paul%s-crt",
		},
		// suffix as a name
		{
			username:                  "",
			email:                     "paul@hotel.com",
			expectedCompliantUsername: "%s-crt",
		},
	}

	// Create another UserSignups with a forbidden suffix
	for _, suffix := range []string{"admin"} {
		for _, testCase := range testCasesSuffix {
			assertComplaintUsernameForNewSignup(t, awaitilities, TestCase{
				username:                  testCase.username + suffix,
				email:                     testCase.email,
				expectedCompliantUsername: fmt.Sprintf(testCase.expectedCompliantUsername, suffix),
			})
		}
	}
}

func assertComplaintUsernameForNewSignup(t *testing.T, awaitilities wait.Awaitilities, testCase TestCase) {
	user := NewSignupRequest(awaitilities).
		Username(testCase.username).
		Email(testCase.email).
		ManuallyApprove().
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)

	require.Equal(t, testCase.expectedCompliantUsername, user.UserSignup.Status.CompliantUsername)
}
