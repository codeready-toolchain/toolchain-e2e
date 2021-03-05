package cmd

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	"github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/setup/terminal"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/wait"

	"github.com/gosuri/uiprogress"
	"github.com/gosuri/uitable/util/strutil"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8swait "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var kubeconfig string
var verbose bool
var hostOperatorNamespace string
var memberOperatorNamespace string

// Execute the setup command to fill a cluster with as many users as requested.
// The command uses the default `$KUBECONFIG` or `<home>/.kube/config` unless a path is specified.
func Execute() {
	cmd := &cobra.Command{
		Use:           "setup <user_account_number>",
		Short:         "setup a clutser with the given number of user accounts",
		SilenceErrors: true,
		SilenceUsage:  false,
		Args:          cobra.ExactArgs(1),
		Run:           setup,
	}
	cmd.Flags().StringVarP(&kubeconfig, "kubeconfig", "", "", "(optional) absolute path to the kubeconfig file")
	cmd.Flags().BoolVar(&verbose, "verbose", false, "if 'debug' traces should be displayed in the console (false by default)")
	cmd.Flags().StringVar(&hostOperatorNamespace, "host-operator-namespace", "toolchain-host-operator", "the namespace of Host operator ('toolchain-host-operator' by default)")
	cmd.Flags().StringVar(&memberOperatorNamespace, "member-operator-namespace", "toolchain-member-operator", "the namespace of the Member operator ('toolchain-member-operator' by default)")

	if err := cmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func setup(cmd *cobra.Command, args []string) {
	cmd.SilenceUsage = true
	term := terminal.New(cmd.InOrStdin, cmd.OutOrStdout, verbose)
	cl, config, _, err := configuration.NewClient(term, kubeconfig)
	if err != nil {
		term.Fatalf(err, "cannot create client")
	}

	memberClusterName, err := getMemberClusterName(cl)
	if err != nil {
		term.Fatalf(err, "unable to lookup member cluster name")
	}
	count, err := strconv.Atoi(args[0])
	if err != nil {
		term.Fatalf(err, "invalid number of user accounts to provision: '%s'", args[0])
	}
	if !term.PromptBoolf("👤 provision %d users on %s", count, config.Host) {
		return
	}

	// provision the users
	term.Infof("🍿 provisioning users...")

	uiprogress.Start()

	// start the progress bars in go routines
	var wg sync.WaitGroup
	usersignupBar := uiprogress.AddBar(count).AppendCompleted().PrependFunc(func(b *uiprogress.Bar) string {
		return strutil.PadLeft("user signups", 15, ' ')
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for usersignupBar.Incr() {
			username := fmt.Sprintf("johnsmith-%04d", usersignupBar.Current())
			userSignup := newUserSignup(username, memberClusterName)
			if err := cl.Create(context.TODO(), userSignup); err != nil {
				term.Fatalf(err, "failed to provision user '%s'", username)
			}
			time.Sleep(time.Millisecond * 20)
		}
	}()
	idlerBar := uiprogress.AddBar(count).AppendCompleted().PrependFunc(func(b *uiprogress.Bar) string {
		return strutil.PadLeft("idler updates", 15, ' ')
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for idlerBar.Incr() {
			username := fmt.Sprintf("johnsmith-%04d", idlerBar.Current())
			if err := updateUserIdlersTimeout(term, cl, username, 15*time.Second); err != nil {
				term.Fatalf(err, "failed to update idlers for user '%s'", username)
			}
			time.Sleep(time.Millisecond * 20)
		}
	}()
	wg.Wait()
	uiprogress.Stop()
	term.Infof("🏁 done provisioning users")
	term.Infof("👋 have fun!")
}

func newUserSignup(username, memberClusterName string) *toolchainv1alpha1.UserSignup {
	return &toolchainv1alpha1.UserSignup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: hostOperatorNamespace,
			Name:      username,
			Annotations: map[string]string{
				toolchainv1alpha1.UserSignupUserEmailAnnotationKey: fmt.Sprintf("%s@test.com", username),
			},
			Labels: map[string]string{
				toolchainv1alpha1.UserSignupUserEmailHashLabelKey: md5.CalcMd5(fmt.Sprintf("%s@test.com", username)),
			},
		},
		Spec: toolchainv1alpha1.UserSignupSpec{
			Username:      username,
			UserID:        username,
			Approved:      true,
			TargetCluster: memberClusterName,
		},
	}
}

func updateUserIdlersTimeout(term terminal.Terminal, cl client.Client, username string, timeout time.Duration) error {
	for _, suffix := range []string{"code", "dev", "stage"} { // TODO: hard coded suffixes, we could probably get them from the tier instead
		idlerName := fmt.Sprintf("%s-%s", username, suffix)
		idler, err := getIdler(cl, idlerName)
		if err != nil {
			return err
		}
		idler.Spec.TimeoutSeconds = int32(timeout.Seconds())
		if err = cl.Update(context.TODO(), idler); err != nil {
			return err
		}
	}
	return nil
}

const (
	defaultRetryInterval = time.Millisecond * 100
	defaultTimeout       = time.Second * 60
)

func getIdler(cl client.Client, name string) (*toolchainv1alpha1.Idler, error) {
	idler := &toolchainv1alpha1.Idler{}
	err := k8swait.Poll(defaultRetryInterval, defaultTimeout, func() (bool, error) {
		err := cl.Get(context.TODO(), types.NamespacedName{
			Name: name,
		}, idler)
		if errors.IsNotFound(err) {
			return false, nil
		} else if err != nil {
			return false, err
		}
		// check the status conditions, wait until the idler is "Ready/True"
		return test.ContainsCondition(idler.Status.Conditions, testsupport.Running()), nil

	})
	return idler, err
}

func getMemberClusterName(cl client.Client) (string, error) {
	var memberCluster v1alpha1.ToolchainCluster
	err := k8swait.Poll(defaultRetryInterval, defaultTimeout, func() (bool, error) {
		clusters := &v1alpha1.ToolchainClusterList{}
		if err := cl.List(context.TODO(), clusters, client.InNamespace(hostOperatorNamespace), client.MatchingLabels{
			"namespace": memberOperatorNamespace,
			"type":      "member",
		}); err != nil {
			return false, err
		}
		for _, cluster := range clusters.Items {
			if containsClusterCondition(cluster.Status.Conditions, wait.ReadyToolchainCluster) {
				memberCluster = cluster
				return true, nil
			}
		}
		return false, nil
	})
	return memberCluster.Name, err
}

func containsClusterCondition(conditions []v1alpha1.ToolchainClusterCondition, contains *v1alpha1.ToolchainClusterCondition) bool {
	if contains == nil {
		return true
	}
	for _, c := range conditions {
		if c.Type == contains.Type {
			return contains.Status == c.Status
		}
	}
	return false
}
