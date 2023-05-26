package e2e

import (
	"context"
	"testing"

	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	config "sigs.k8s.io/controller-runtime/pkg/client/config"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	userv1 "github.com/openshift/api/user/v1"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"

	"k8s.io/client-go/rest"

	testsupport "github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"
)

func TestUserCreatingRoleBindings(t *testing.T) {

	awaitilities := testsupport.WaitForDeployments(t)
	memberAwait := awaitilities.Member1()

	config, err := config.GetConfig()
	require.NoError(t, err)

	s := runtime.NewScheme()
	err = userv1.Install(s)
	require.NoError(t, err)

	// Create and approve a sandbox user
	testsupport.NewSignupRequest(awaitilities).
		Username("harleyquinn").
		ManuallyApprove().
		TargetCluster(memberAwait).
		EnsureMUR().
		RequireConditions(wait.ConditionSet(wait.Default(), wait.ApprovedByAdmin())...).
		Execute(t)

	//create a non-sandbox user
	nonsandboxUser := userv1.User{
		ObjectMeta: metav1.ObjectMeta{
			Name: "joker",
		},
	}
	err = memberAwait.Client.Create(context.TODO(), &nonsandboxUser)
	require.NoError(t, err)

	// Create a rolebinding to let non-sandbox user create rolebinding in another's ns
	nonsandboxRoleBinding := rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			Kind: "RoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "non-sanbox-user",
			Namespace: "harleyquinn-dev",
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "admin",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     "User",
				APIGroup: "rbac.authorization.k8s.io",
				Name:     "joker",
			},
		},
	}
	err = memberAwait.Client.Create(context.TODO(), &nonsandboxRoleBinding)
	require.NoError(t, err)

	defer deleteUser(t, memberAwait.Client, nonsandboxUser, nonsandboxRoleBinding)

	t.Run("sandbox user creating different rolebindings", func(t *testing.T) {
		config.Impersonate = rest.ImpersonationConfig{
			UserName: "harleyquinn",
		}
		clientset, err := runtimeclient.New(config, runtimeclient.Options{})
		require.NoError(t, err)
		// get all the rolebindings for the test case
		roleBindings := createRoleBindingsForTest("harleyquinn-dev", "sanbox-user")
		require.Equal(t, 5, len(roleBindings))
		// map the test cases with the expected error
		tests := map[string]struct {
			RoleBinding   rbacv1.RoleBinding
			ExpectedError string
		}{
			"access to all users should be denied": {
				RoleBinding:   roleBindings[0],
				ExpectedError: "this is a Dev Sandbox enforced restriction. you are trying to create a rolebinding giving access to a larger audience, i.e : system:authenticated",
			},
			"access all users with : should be denied": {
				RoleBinding:   roleBindings[1],
				ExpectedError: "this is a Dev Sandbox enforced restriction. you are trying to create a rolebinding giving access to a larger audience, i.e : system:authenticated:",
			},
			"access to all service accounts should be denied": {
				RoleBinding:   roleBindings[2],
				ExpectedError: "this is a Dev Sandbox enforced restriction. you are trying to create a rolebinding giving access to a larger audience, i.e : system:serviceaccounts",
			},
			"access to all service accounts with : should be denied": {
				RoleBinding:   roleBindings[3],
				ExpectedError: "this is a Dev Sandbox enforced restriction. you are trying to create a rolebinding giving access to a larger audience, i.e : system:serviceaccounts:",
			},
			"access to a particular user should be allowed": {
				RoleBinding:   roleBindings[4],
				ExpectedError: "",
			},
		}
		for k, tc := range tests {
			t.Run(k, func(t *testing.T) {
				// when
				err = clientset.Create(context.TODO(), &tc.RoleBinding)
				// then
				if tc.ExpectedError != "" {
					require.Contains(t, err.Error(), tc.ExpectedError)
				} else {
					require.NoError(t, err)
				}
			})
		}
	})

	t.Run("service account trying to create a rolebinding giving access to all users should be allowed", func(t *testing.T) {
		//using hostawailities which using e2e-service-account
		// get all the rolebindings for the test case
		roleBindings := createRoleBindingsForTest("harleyquinn-dev", "e2e-sa")
		require.Equal(t, 5, len(roleBindings))
		// map the test cases with the expected error
		tests := map[string]struct {
			RoleBinding   rbacv1.RoleBinding
			ExpectedError string
		}{
			"access to all users should be allowed": {
				RoleBinding:   roleBindings[0],
				ExpectedError: "",
			},
			"access all users with : should be allowed": {
				RoleBinding:   roleBindings[1],
				ExpectedError: "",
			},
			"access to all service accounts should be allowed": {
				RoleBinding:   roleBindings[2],
				ExpectedError: "",
			},
			"access to all service accounts with : should be allowed": {
				RoleBinding:   roleBindings[3],
				ExpectedError: "",
			},
			"access to a particular user should be allowed": {
				RoleBinding:   roleBindings[4],
				ExpectedError: "",
			},
		}
		for k, tc := range tests {
			t.Run(k, func(t *testing.T) {
				//when
				err = awaitilities.Host().Client.Create(context.TODO(), &tc.RoleBinding)
				// then
				if tc.ExpectedError != "" {
					require.Contains(t, err.Error(), tc.ExpectedError)
				} else {
					require.NoError(t, err)
				}
			})
		}
	})

	t.Run("non-sandbox user creating various rolebindings", func(t *testing.T) {
		config.Impersonate = rest.ImpersonationConfig{
			UserName: "joker",
		}
		clientset, err := runtimeclient.New(config, runtimeclient.Options{})
		// get all the rolebindings for the test case
		roleBindings := createRoleBindingsForTest("harleyquinn-dev", "non-sanbox-user")
		require.Equal(t, 5, len(roleBindings))
		// map the test cases with the expected rolebinding and error
		tests := map[string]struct {
			RoleBinding   rbacv1.RoleBinding
			ExpectedError string
		}{
			"access to all users should be allowed": {
				RoleBinding:   roleBindings[0],
				ExpectedError: "",
			},
			"access all users with : should be allowed": {
				RoleBinding:   roleBindings[1],
				ExpectedError: "",
			},
			"access to all service accounts should be allowed": {
				RoleBinding:   roleBindings[2],
				ExpectedError: "",
			},
			"access to all service accounts with : should be allowed": {
				RoleBinding:   roleBindings[3],
				ExpectedError: "",
			},
			"access to a particular user should be allowed": {
				RoleBinding:   roleBindings[4],
				ExpectedError: "",
			},
		}
		for k, tc := range tests {
			t.Run(k, func(t *testing.T) {
				// when
				err = clientset.Create(context.TODO(), &tc.RoleBinding)
				// then
				if tc.ExpectedError != "" {
					require.Contains(t, err.Error(), tc.ExpectedError)
				} else {
					require.NoError(t, err)
				}
			})
		}

	})

}

func deleteUser(t *testing.T, cl runtimeclient.Client, user userv1.User, rb rbacv1.RoleBinding) {
	err := cl.Delete(context.TODO(), &rb)
	require.NoError(t, err)
	err = cl.Delete(context.TODO(), &user)
	require.NoError(t, err)
}

func createRoleBindingsForTest(ns string, createdBy string) []rbacv1.RoleBinding {
	return []rbacv1.RoleBinding{
		{
			TypeMeta: metav1.TypeMeta{
				Kind: "RoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wide-access-" + createdBy,
				Namespace: ns,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "edit",
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:     "Group",
					APIGroup: "rbac.authorization.k8s.io",
					Name:     "system:authenticated",
				},
			},
		},
		{
			TypeMeta: metav1.TypeMeta{
				Kind: "RoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wide-access-2-" + createdBy,
				Namespace: ns,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "edit",
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:     "Group",
					APIGroup: "rbac.authorization.k8s.io",
					Name:     "system:authenticated:",
				},
			},
		},
		{
			TypeMeta: metav1.TypeMeta{
				Kind: "RoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wide-access-sa-" + createdBy,
				Namespace: ns,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "edit",
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:     "Group",
					APIGroup: "rbac.authorization.k8s.io",
					Name:     "system:serviceaccounts",
				},
			},
		},
		{
			TypeMeta: metav1.TypeMeta{
				Kind: "RoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "wide-access-sa-2-" + createdBy,
				Namespace: ns,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "edit",
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:     "Group",
					APIGroup: "rbac.authorization.k8s.io",
					Name:     "system:serviceaccounts:",
				},
			},
		},
		{
			TypeMeta: metav1.TypeMeta{
				Kind: "RoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "user-access-" + createdBy,
				Namespace: ns,
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "ClusterRole",
				Name:     "edit",
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:     "User",
					APIGroup: "rbac.authorization.k8s.io",
					Name:     "johnsmith",
				},
			},
		},
	}
}
