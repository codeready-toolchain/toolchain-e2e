package verify

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/cluster"
	"github.com/codeready-toolchain/toolchain-common/pkg/hash"
	"github.com/codeready-toolchain/toolchain-common/pkg/states"
	"github.com/codeready-toolchain/toolchain-e2e/test/migration"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/cleanup"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/space"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestAfterMigration(t *testing.T) {
	// given
	awaitilities := WaitForDeployments(t)
	// increase timeout to be sure that the operators had enough time to properly initialize and reconcile all present resources
	awaitilities = wait.NewAwaitilities(
		awaitilities.Host().WithRetryOptions(wait.TimeoutOption(wait.DefaultTimeout*2)),
		awaitilities.Member1().WithRetryOptions(wait.TimeoutOption(wait.DefaultTimeout*2)),
		awaitilities.Member2().WithRetryOptions(wait.TimeoutOption(wait.DefaultTimeout*2)))

	// run all the verify functions concurrently
	// to ensure that the objects provisioned with the "old" operator versions are not altered in an unexpected way by the new operator version (the changes are backward compatible)
	runVerifyFunctions(t, awaitilities)

	t.Run("run migration setup with new operator versions for compatibility", func(t *testing.T) {
		// We need to run the migration setup part to ensure the compatibility with both versions of the sandbox (the old one as well as the new one)
		// Consider this:
		//    1. there is a running version of host/member-operator that populates the `UserAccount.Spec.NSTemplateSet` field
		//    2. the `migration setup logic` checks and expects that this field is populated
		//    3. there is a migration PR that drops usage of that `UserAccount.Spec.NSTemplateSet` field so it won't be populated any more
		//    4. there is also paired e2e PR that modifies the `verify migration test` so it doesn't expect the field to be populated
		//    5. the tests are green - so far so good.
		//    6. now, when the PRs are merged, we need to make sure that the setup will pass also for all the following PRs. If the logic wasn't compatible
		//       then the next PRs would fail in the `migration setup logic` because it would still contain the logic that would check and expect
		//       `UserAccount.Spec.NSTemplateSet` to be populated.
		//
		// That use-case could fit to any situation where the setup part relies on a functionality/feature that is present in the current version of operators
		// but won't be present in the next one.
		//
		// This is based on the assumption that everything what is merged in master works as expected. This means that we can "just" create either UserSignup
		// or Space and wait for the provisioned status (or any other status/state that signs that the process is done). We don't have to verify the actual
		// content of the resources, nor labels, etc... because it was already verified when the PR that was merged. If we agree on such a generic setup logic,
		// then we can easily make sure that it's fully compatible with both versions of Dev Sandbox.
		runner := migration.SetupMigrationRunner{
			Awaitilities: awaitilities,
			WithCleanup:  false,
		}

		runner.Run(t)

		// run all the verify functions concurrently
		// to ensure that everything was provisioned as expected using the new operator versions.
		runVerifyFunctions(t, awaitilities)
	})
}

func runVerifyFunctions(t *testing.T, awaitilities wait.Awaitilities) {
	// check MUR migrations and get Signups for the users provisioned in the setup part
	t.Log("checking MUR Migrations")
	provisionedSignup := checkMURMigratedAndGetSignup(t, awaitilities.Host(), migration.ProvisionedUser)
	secondMemberProvisionedSignup := checkMURMigratedAndGetSignup(t, awaitilities.Host(), migration.SecondMemberProvisionedUser)
	appstudioProvisionedSignup := checkMURMigratedAndGetSignup(t, awaitilities.Host(), migration.AppStudioProvisionedUser)

	// note: listing banned/deactivated UserSignups should be done as part of setup because the tests are run in parallel and there can be multiple banned/deactivated UserSignups at that point which could lead to test flakiness
	deactivatedSignup := listAndGetSignupWithState(t, awaitilities.Host(), toolchainv1alpha1.UserSignupStateLabelValueDeactivated)
	bannedSignup := listAndGetSignupWithState(t, awaitilities.Host(), toolchainv1alpha1.UserSignupStateLabelValueBanned)

	var wg sync.WaitGroup

	// prepare all functions to verify the state of the Signups and Spaces
	toRun := []func(){
		// Spaces
		func() { verifyAppStudioProvisionedSpace(t, awaitilities) },
		func() { verifySecondMemberProvisionedSpace(t, awaitilities) },
		func() { verifyProvisionedSubSpace(t, awaitilities) },
		// UserSignups
		func() { verifyProvisionedSignup(t, awaitilities, provisionedSignup) },
		func() { verifySecondMemberProvisionedSignup(t, awaitilities, secondMemberProvisionedSignup) },
		func() { verifyAppStudioProvisionedSignup(t, awaitilities, appstudioProvisionedSignup) },
		func() { verifyDeactivatedSignup(t, awaitilities, deactivatedSignup) },
		func() { verifyBannedSignup(t, awaitilities, bannedSignup) },
		func() { verifyAdditionalDeploymentsCreatedUsingSSA(t, &awaitilities) },
		func() { verifyNSTemplateTiers(t, &awaitilities) },
		func() { verifyResourcesDeployedUsingSSA(t, &awaitilities) },
	}

	// when & then - run all functions in parallel
	for _, funcToRun := range toRun {
		wg.Add(1)
		go func(run func()) {
			defer wg.Done()
			run()
		}(funcToRun)
	}

	wg.Wait()

	cleanup.ExecuteAllCleanTasks(t)

	// wait until the ToolchainStatus is updated to make sure that all counters are in sync
	_, err := awaitilities.Host().WaitForToolchainStatus(t, wait.UntilToolchainStatusUpdatedAfter(time.Now()))
	require.NoError(t, err)
}

func verifyAppStudioProvisionedSpace(t *testing.T, awaitilities wait.Awaitilities) {
	space, _ := VerifyResourcesProvisionedForSpace(t, awaitilities, migration.ProvisionedAppStudioSpace)
	userSignupForSpace := checkMURMigratedAndGetSignup(t, awaitilities.Host(), migration.ProvisionedAppStudioSpace)
	cleanup.AddCleanTasks(t, awaitilities.Host().Client, space)
	cleanup.AddCleanTasks(t, awaitilities.Host().Client, userSignupForSpace)
}

func verifySecondMemberProvisionedSpace(t *testing.T, awaitilities wait.Awaitilities) {
	space, _ := VerifyResourcesProvisionedForSpace(t, awaitilities, migration.SecondMemberProvisionedSpace)
	userSignupForSpace := checkMURMigratedAndGetSignup(t, awaitilities.Host(), migration.SecondMemberProvisionedSpace)
	cleanup.AddCleanTasks(t, awaitilities.Host().Client, space)
	cleanup.AddCleanTasks(t, awaitilities.Host().Client, userSignupForSpace)
}

func verifyProvisionedSubSpace(t *testing.T, awaitilities wait.Awaitilities) {
	parentSpace, _ := VerifyResourcesProvisionedForSpace(t, awaitilities, migration.ProvisionedParentSpace)
	userSignupForSpace := checkMURMigratedAndGetSignup(t, awaitilities.Host(), migration.ProvisionedParentSpace)
	targetClusterRoles := []string{cluster.RoleLabel(cluster.Tenant)}
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member2()
	memberCluster, found, err := hostAwait.GetToolchainCluster(t, memberAwait.Namespace, toolchainv1alpha1.ConditionReady)
	require.NoError(t, err)
	require.True(t, found)
	subSpaceNamespace := GetDefaultNamespace(parentSpace.Status.ProvisionedNamespaces)

	subSpace, err := awaitilities.Host().WaitForSubSpace(t,
		migration.ProvisionedSpaceRequest,
		subSpaceNamespace,
		parentSpace.GetName(),
		wait.UntilSpaceHasTargetClusterRoles(targetClusterRoles),
		wait.UntilSpaceHasTier("appstudio-env"),
		wait.UntilSpaceHasAnyProvisionedNamespaces())
	require.NoError(t, err)

	spaceRequest, err := memberAwait.WaitForSpaceRequest(t,
		types.NamespacedName{
			Namespace: subSpaceNamespace,
			Name:      migration.ProvisionedSpaceRequest,
		},
		wait.UntilSpaceRequestHasStatusTargetClusterURL(memberCluster.Status.APIEndpoint),
		wait.UntilSpaceRequestHasNamespaceAccess(subSpace),
		wait.UntilSpaceRequestHasConditions(wait.Provisioned()),
	)
	require.NoError(t, err)
	VerifyNamespaceAccessForSpaceRequest(t, memberAwait.Client, spaceRequest)

	cleanup.AddCleanTasks(t, hostAwait.Client, parentSpace)
	cleanup.AddCleanTasks(t, hostAwait.Client, userSignupForSpace)
}

func verifyProvisionedSignup(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup) {
	cleanup.AddCleanTasks(t, awaitilities.Host().Client, signup)
	VerifyResourcesProvisionedForSignup(t, awaitilities, signup)
	DeactivateAndCheckUser(t, awaitilities, signup)
	ReactivateAndCheckUser(t, awaitilities, signup)
}

func verifySecondMemberProvisionedSignup(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup) {
	cleanup.AddCleanTasks(t, awaitilities.Host().Client, signup)
	VerifyResourcesProvisionedForSignup(t, awaitilities, signup)
	CreateBannedUser(t, awaitilities.Host(), signup.Spec.IdentityClaims.Email)
}

func verifyAppStudioProvisionedSignup(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup) {
	cleanup.AddCleanTasks(t, awaitilities.Host().Client, signup)
	VerifyResourcesProvisionedForSignupWithTiers(t, awaitilities, signup, "deactivate30", "appstudio")
}

func verifyDeactivatedSignup(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup) {
	cleanup.AddCleanTasks(t, awaitilities.Host().Client, signup)

	_, err := awaitilities.Host().WaitForUserSignup(t, signup.Name,
		wait.UntilUserSignupContainsConditions(wait.ConditionSet(wait.Default(), wait.DeactivatedWithoutPreDeactivation())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueDeactivated))
	require.NoError(t, err)
	require.True(t, states.Deactivated(signup), "usersignup should be deactivated")

	err = awaitilities.Host().WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, migration.DeactivatedUser)
	require.NoError(t, err)

	err = awaitilities.Host().WaitUntilSpaceAndSpaceBindingsDeleted(t, migration.DeactivatedUser)
	require.NoError(t, err)

	ReactivateAndCheckUser(t, awaitilities, signup)
}

func verifyBannedSignup(t *testing.T, awaitilities wait.Awaitilities, signup *toolchainv1alpha1.UserSignup) {
	hostAwait := awaitilities.Host()
	cleanup.AddCleanTasks(t, hostAwait.Client, signup)

	// verify that it's still banned
	_, err := hostAwait.WaitForUserSignup(t, signup.Name,
		wait.UntilUserSignupHasConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin(), wait.Banned())...),
		wait.UntilUserSignupHasStateLabel(toolchainv1alpha1.UserSignupStateLabelValueBanned))
	require.NoError(t, err)

	err = hostAwait.WaitUntilMasterUserRecordAndSpaceBindingsDeleted(t, migration.BannedUser)
	require.NoError(t, err)

	err = awaitilities.Host().WaitUntilSpaceAndSpaceBindingsDeleted(t, migration.BannedUser)
	require.NoError(t, err)

	// get the BannedUser resource
	matchEmailHash := client.MatchingLabels{
		toolchainv1alpha1.BannedUserEmailHashLabelKey: hash.EncodeString(signup.Spec.IdentityClaims.Email),
	}
	bannedUsers := &toolchainv1alpha1.BannedUserList{}
	err = hostAwait.Client.List(context.TODO(), bannedUsers, client.InNamespace(hostAwait.Namespace), matchEmailHash)
	require.NoError(t, err)
	require.Len(t, bannedUsers.Items, 1)

	// Unban the user by deleting the BannedUser resource
	err = hostAwait.Client.Delete(context.TODO(), &bannedUsers.Items[0])
	require.NoError(t, err)

	// verify that it's unbanned
	VerifyResourcesProvisionedForSignup(t, awaitilities, signup)
}

func verifyAdditionalDeploymentsCreatedUsingSSA(t *testing.T, awaitilities *wait.Awaitilities) {
	testDeployment := func(t *testing.T, a *wait.Awaitility, deploymentName, originalFieldManager, expectedFieldManager string) {
		_, err := wait.For(t, a, &appsv1.Deployment{}).WithNameMatching(deploymentName, func(d *appsv1.Deployment) bool {
			var applyEntry *metav1.ManagedFieldsEntry
			var updateEntry *metav1.ManagedFieldsEntry
			for _, mf := range d.ManagedFields {
				if mf.Manager == expectedFieldManager {
					applyEntry = &mf
				}
				if mf.Manager == originalFieldManager {
					updateEntry = &mf
				}
			}
			return applyEntry != nil && applyEntry.Operation == metav1.ManagedFieldsOperationApply && updateEntry == nil
		})
		require.NoError(t, err)
	}

	t.Run("verify registration service deployed using SSA in host", func(t *testing.T) {
		testDeployment(t, awaitilities.Host().Awaitility, "registration-service", "host-operator", "kubesaw-host-operator")
	})

	// no webhook is deployed in member2, see the "create-host-resources" make target
	t.Run("verify webhook deployed using SSA in member1", func(t *testing.T) {
		testDeployment(t, awaitilities.Member1().Awaitility, "member-operator-webhook", "member-operator", "kubesaw-member-operator")
	})

	t.Run("verify autoscaler deployed using SSA in member1", func(t *testing.T) {
		testDeployment(t, awaitilities.Member1().Awaitility, "autoscaling-buffer", "member-operator", "kubesaw-member-operator")
	})

	t.Run("verify autoscaler deployed using SSA in member2", func(t *testing.T) {
		testDeployment(t, awaitilities.Member2().Awaitility, "autoscaling-buffer", "member-operator", "kubesaw-member-operator")
	})
}

func verifyNSTemplateTiers(t *testing.T, awaitilities *wait.Awaitilities) {
	// Let's make sure we have the correct idea about the NSTemplateTIers
	// present in the cluster.
	//
	// We need to make sure that the cluster contains exactly the tiers we expect
	// (wait.E2eNSTemplateTiers) and also that all the bundled NSTemplateTiers
	// are annotated as such in the cluster (wait.BundledNSTemplateTiers).
	//
	// This makes sure that the setup in the cluster is exactly how the e2e tests
	// expect it to be.

	list := &toolchainv1alpha1.NSTemplateTierList{}
	require.NoError(t, awaitilities.Host().Client.List(context.TODO(), list, client.InNamespace(awaitilities.Host().Namespace)))

	assert.Len(t, list.Items, len(wait.AllE2eNSTemplateTiers))

	bundledInCluster := []string{}
	customInCluster := []string{}

	for _, tier := range list.Items {
		if tier.Annotations[toolchainv1alpha1.BundledAnnotationKey] == "host-operator" {
			bundledInCluster = append(bundledInCluster, tier.Name)
		} else {
			customInCluster = append(customInCluster, tier.Name)
		}
	}

	assert.ElementsMatch(t, wait.BundledNSTemplateTiers, bundledInCluster)
	assert.ElementsMatch(t, wait.CustomNSTemplateTiers, customInCluster)
}

func verifyResourcesDeployedUsingSSA(t *testing.T, awaitilities *wait.Awaitilities) {
	testList := func(t *testing.T, obj client.Object, isEligible func(client.Object) bool) {
		t.Helper()
		assert.EventuallyWithT(t, func(t *assert.CollectT) {
			a := awaitilities.Host().Awaitility
			list := &unstructured.UnstructuredList{}
			gvks, _, err := a.Client.Scheme().ObjectKinds(obj)
			require.NoError(t, err)
			require.Len(t, gvks, 1)
			list.SetGroupVersionKind(gvks[0])
			require.NoError(t, a.Client.List(context.TODO(), list, client.InNamespace(a.Namespace)))

			for _, o := range list.Items {
				if !isEligible(&o) {
					continue
				}
				var applyEntry *metav1.ManagedFieldsEntry
				for _, mf := range o.GetManagedFields() {
					if mf.Manager == "kubesaw-host-operator" {
						applyEntry = &mf
					}
				}

				// note that we only check for the presence of the Apply operation here. Unlike in the case of
				// the deployments tested above, the NSTemplateTiers are updated by the controller, so we're going
				// to see updates made by it.
				require.NotNil(t, applyEntry, "NSTemplateTier '%s' doesn't have the expected Apply operation in the managed fields", o.GetName())
				assert.Equal(t, metav1.ManagedFieldsOperationApply, applyEntry.Operation)
			}
		}, 1*time.Minute, 1*time.Second)
	}

	t.Run("verify bundled UserTiers deployed using SSA", func(t *testing.T) {
		testList(t, &toolchainv1alpha1.UserTier{}, func(_ client.Object) bool {
			return true
		})
	})

	t.Run("verify bundled NSTemplateTiers deployed using SSA", func(t *testing.T) {
		testList(t, &toolchainv1alpha1.NSTemplateTier{}, func(o client.Object) bool {
			return o.GetAnnotations()[toolchainv1alpha1.BundledAnnotationKey] == "host-operator"
		})
	})
}

func checkMURMigratedAndGetSignup(t *testing.T, hostAwait *wait.HostAwaitility, murName string) *toolchainv1alpha1.UserSignup {
	provisionedMur, err := hostAwait.WithRetryOptions(wait.TimeoutOption(time.Second*120)).WaitForMasterUserRecord(t, murName,
		wait.UntilMasterUserRecordHasCondition(wait.Provisioned()),
		wait.UntilMasterUserRecordHasNoTierHashLabel(), // after migration there should be no tier hash label so we should wait for that to confirm migration is completed before proceeding
	)
	require.NoError(t, err)

	signup, err := hostAwait.WaitForUserSignup(t, provisionedMur.Labels[toolchainv1alpha1.MasterUserRecordOwnerLabelKey])
	require.NoError(t, err)

	checkMURMigrated(t, provisionedMur)

	return signup
}

// checkMURMigrated ensures that all MURs are correctly migrated
func checkMURMigrated(t *testing.T, mur *toolchainv1alpha1.MasterUserRecord) {
	// should have tier name set
	require.NotEmpty(t, mur.Spec.TierName)

	// should not have tier hash label
	require.Empty(t, mur.Labels[fmt.Sprintf("toolchain.dev.openshift.com/%s-tier-hash", mur.Spec.TierName)])

	require.Len(t, mur.Spec.UserAccounts, 1)
}

func listAndGetSignupWithState(t *testing.T, hostAwait *wait.HostAwaitility, state string) *toolchainv1alpha1.UserSignup {
	userSignups := &toolchainv1alpha1.UserSignupList{}
	err := hostAwait.Client.List(context.TODO(), userSignups, client.InNamespace(hostAwait.Namespace), client.MatchingLabels{toolchainv1alpha1.UserSignupStateLabelKey: state})
	require.NoError(t, err)

	require.Len(t, userSignups.Items, 1)
	return &userSignups.Items[0]
}
