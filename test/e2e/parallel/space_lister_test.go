package parallel

import (
	"context"
	"fmt"
	"testing"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/api/v1alpha1"
	commonproxy "github.com/codeready-toolchain/toolchain-common/pkg/proxy"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	. "github.com/codeready-toolchain/toolchain-e2e/testsupport/proxy"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/gofrs/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpaceLister(t *testing.T) {
	t.Parallel()
	// given
	awaitilities := WaitForDeployments(t)
	hostAwait := awaitilities.Host()
	memberAwait := awaitilities.Member1()
	memberAwait2 := awaitilities.Member2()

	SetAppstudioConfig(t, hostAwait, memberAwait)

	t.Logf("Proxy URL: %s", hostAwait.APIProxyURL)

	users := map[string]*ProxyUser{
		"car": {
			ExpectedMemberCluster: memberAwait,
			Username:              "car",
			IdentityID:            uuid.Must(uuid.NewV4()),
		},
		"bus": {
			ExpectedMemberCluster: memberAwait2,
			Username:              "bus",
			IdentityID:            uuid.Must(uuid.NewV4()),
		},
		"bicycle": {
			ExpectedMemberCluster: memberAwait,
			Username:              "road.bicycle", // contains a '.' that is valid in the Username but should not be in the impersonation header since it should use the compliant Username
			IdentityID:            uuid.Must(uuid.NewV4()),
		},
	}
	appStudioTierRolesWSOption := commonproxy.WithAvailableRoles([]string{"admin", "contributor", "maintainer"})

	// create the users before the subtests, so they exist for the duration of the whole test
	for _, user := range users {
		CreateAppStudioUser(t, awaitilities, user)
	}

	users["car"].ShareSpaceWith(t, hostAwait, users["bus"])
	users["car"].ShareSpaceWith(t, hostAwait, users["bicycle"])
	users["bus"].ShareSpaceWith(t, hostAwait, users["bicycle"])

	t.Run("car lists workspaces", func(t *testing.T) {
		// when
		workspaces := users["car"].ListWorkspaces(t, hostAwait)

		// then
		// car should see only car's workspace
		require.Len(t, workspaces, 1)
		verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["car"], commonproxy.WithType("home")), workspaces...)
	})

	t.Run("car gets workspaces", func(t *testing.T) {
		t.Run("can get car workspace", func(t *testing.T) {
			// when
			workspace, err := users["car"].GetWorkspace(t, hostAwait, users["car"].CompliantUsername)

			// then
			require.NoError(t, err)
			verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["car"], commonproxy.WithType("home"), appStudioTierRolesWSOption), *workspace)
		})

		t.Run("cannot get bus workspace", func(t *testing.T) {
			// when
			workspace, err := users["car"].GetWorkspace(t, hostAwait, users["bus"].CompliantUsername)

			// then
			require.EqualError(t, err, "the server could not find the requested resource (get workspaces.toolchain.dev.openshift.com bus)")
			assert.Empty(t, workspace)
		})
	})

	t.Run("bus lists workspaces", func(t *testing.T) {
		// when
		workspaces := users["bus"].ListWorkspaces(t, hostAwait)

		// then
		// bus should see both its own and car's workspace
		require.Len(t, workspaces, 2)
		verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["bus"], commonproxy.WithType("home")), workspaces...)
		verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["car"]), workspaces...)
	})

	t.Run("bus gets workspaces", func(t *testing.T) {
		t.Run("can get bus workspace", func(t *testing.T) {
			// when
			busWS, err := users["bus"].GetWorkspace(t, hostAwait, users["bus"].CompliantUsername)

			// then
			require.NoError(t, err)
			verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["bus"], commonproxy.WithType("home"), appStudioTierRolesWSOption), *busWS)
		})

		t.Run("can get car workspace(bus)", func(t *testing.T) {
			// when
			carWS, err := users["bus"].GetWorkspace(t, hostAwait, users["car"].CompliantUsername)

			// then
			require.NoError(t, err)
			verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["car"], appStudioTierRolesWSOption), *carWS)
		})
	})

	t.Run("bicycle lists workspaces", func(t *testing.T) {
		// when
		workspaces := users["bicycle"].ListWorkspaces(t, hostAwait)

		// then
		// car should see only car's workspace
		require.Len(t, workspaces, 3)
		verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["bicycle"], commonproxy.WithType("home")), workspaces...)
		verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["car"]), workspaces...)
		verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["bus"]), workspaces...)
	})

	t.Run("bicycle gets workspaces", func(t *testing.T) {
		t.Run("can get bus workspace", func(t *testing.T) {
			// when
			busWS, err := users["bicycle"].GetWorkspace(t, hostAwait, users["bus"].CompliantUsername)

			// then
			require.NoError(t, err)
			verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["bus"], appStudioTierRolesWSOption), *busWS)
		})

		t.Run("can get car workspace(bicycle)", func(t *testing.T) {
			// when
			carWS, err := users["bicycle"].GetWorkspace(t, hostAwait, users["car"].CompliantUsername)

			// then
			require.NoError(t, err)
			verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["car"], appStudioTierRolesWSOption), *carWS)
		})

		t.Run("can get bicycle workspace", func(t *testing.T) {
			// when
			bicycleWS, err := users["bicycle"].GetWorkspace(t, hostAwait, users["bicycle"].CompliantUsername)

			// then
			require.NoError(t, err)
			verifyHasExpectedWorkspace(t, expectedWorkspaceFor(t, awaitilities.Host(), users["bicycle"], commonproxy.WithType("home"), appStudioTierRolesWSOption), *bicycleWS)
		})
	})

	t.Run("other workspace actions not permitted", func(t *testing.T) {
		t.Run("create not allowed", func(t *testing.T) {
			// given
			workspaceToCreate := expectedWorkspaceFor(t, awaitilities.Host(), users["bus"])
			bicycleCl, err := hostAwait.CreateAPIProxyClient(t, users["bicycle"].Token, hostAwait.APIProxyURL)
			require.NoError(t, err)

			// when
			// bicycle user tries to create a workspace
			err = bicycleCl.Create(context.TODO(), &workspaceToCreate)

			// then
			require.EqualError(t, err, fmt.Sprintf("workspaces.toolchain.dev.openshift.com is forbidden: User \"%s\" cannot create resource \"workspaces\" in API group \"toolchain.dev.openshift.com\" at the cluster scope", users["bicycle"].CompliantUsername))
		})

		t.Run("delete not allowed", func(t *testing.T) {
			// given
			workspaceToDelete, err := users["bicycle"].GetWorkspace(t, hostAwait, users["bicycle"].CompliantUsername)
			require.NoError(t, err)
			bicycleCl, err := hostAwait.CreateAPIProxyClient(t, users["bicycle"].Token, hostAwait.APIProxyURL)
			require.NoError(t, err)

			// bicycle user tries to delete a workspace
			err = bicycleCl.Delete(context.TODO(), workspaceToDelete)

			// then
			require.EqualError(t, err, fmt.Sprintf("workspaces.toolchain.dev.openshift.com \"%[1]s\" is forbidden: User \"%[1]s\" cannot delete resource \"workspaces\" in API group \"toolchain.dev.openshift.com\" at the cluster scope", users["bicycle"].CompliantUsername))
		})

		t.Run("update not allowed", func(t *testing.T) {
			// when
			workspaceToUpdate := expectedWorkspaceFor(t, awaitilities.Host(), users["bicycle"], commonproxy.WithType("home"))
			bicycleCl, err := hostAwait.CreateAPIProxyClient(t, users["bicycle"].Token, hostAwait.APIProxyURL)
			require.NoError(t, err)

			// bicycle user tries to update a workspace
			err = bicycleCl.Update(context.TODO(), &workspaceToUpdate)

			// then
			require.EqualError(t, err, fmt.Sprintf("workspaces.toolchain.dev.openshift.com \"%[1]s\" is forbidden: User \"%[1]s\" cannot update resource \"workspaces\" in API group \"toolchain.dev.openshift.com\" at the cluster scope", users["bicycle"].CompliantUsername))
		})
	})
}

func expectedWorkspaceFor(t *testing.T, hostAwait *wait.HostAwaitility, user *ProxyUser, additionalWSOptions ...commonproxy.WorkspaceOption) toolchainv1alpha1.Workspace {
	space, err := hostAwait.WaitForSpace(t, user.CompliantUsername, wait.UntilSpaceHasAnyTargetClusterSet(), wait.UntilSpaceHasAnyTierNameSet())
	require.NoError(t, err)

	commonWSoptions := []commonproxy.WorkspaceOption{
		commonproxy.WithObjectMetaFrom(space.ObjectMeta),
		commonproxy.WithNamespaces([]toolchainv1alpha1.SpaceNamespace{
			{
				Name: user.CompliantUsername + "-tenant",
				Type: "default",
			},
		}),
		commonproxy.WithOwner(user.Signup.Name),
		commonproxy.WithRole("admin"),
	}
	ws := commonproxy.NewWorkspace(user.CompliantUsername,
		append(commonWSoptions, additionalWSOptions...)...,
	)
	return *ws
}

func verifyHasExpectedWorkspace(t *testing.T, expectedWorkspace toolchainv1alpha1.Workspace, actualWorkspaces ...toolchainv1alpha1.Workspace) {
	for _, actualWorkspace := range actualWorkspaces {
		if actualWorkspace.Name == expectedWorkspace.Name {
			assert.Equal(t, expectedWorkspace.Status, actualWorkspace.Status)
			assert.NotEmpty(t, actualWorkspace.ObjectMeta.ResourceVersion, "Workspace.ObjectMeta.ResourceVersion field is empty: %#v", actualWorkspace)
			assert.NotEmpty(t, actualWorkspace.ObjectMeta.Generation, "Workspace.ObjectMeta.Generation field is empty: %#v", actualWorkspace)
			assert.NotEmpty(t, actualWorkspace.ObjectMeta.CreationTimestamp, "Workspace.ObjectMeta.CreationTimestamp field is empty: %#v", actualWorkspace)
			assert.NotEmpty(t, actualWorkspace.ObjectMeta.UID, "Workspace.ObjectMeta.UID field is empty: %#v", actualWorkspace)
			return
		}
	}
	t.Errorf("expected workspace %s not found", expectedWorkspace.Name)
}
