package cmd

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	"github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/setup/terminal"
	"github.com/codeready-toolchain/toolchain-e2e/setup/user"
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

const usernamePrefix = "zippy"

var kubeconfig string
var verbose bool
var hostOperatorNamespace string
var memberOperatorNamespace string
var templatePath string
var numberOfUsers int
var userBatches int
var delay int
var resourceRate int

// Execute the setup command to fill a cluster with as many users as requested.
// The command uses the default `$KUBECONFIG` or `<home>/.kube/config` unless a path is specified.
func Execute() {
	cmd := &cobra.Command{
		Use:           "setup <template_file>",
		Short:         "setup a cluster with the given number of user accounts",
		SilenceErrors: true,
		SilenceUsage:  false,
		Args:          cobra.ExactArgs(1),
		Run:           setup,
	}

	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "(optional) absolute path to the kubeconfig file")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "if 'debug' traces should be displayed in the console (false by default)")
	cmd.Flags().IntVarP(&numberOfUsers, "users", "u", 3000, "provision N users ('3000' by default)")
	cmd.Flags().IntVarP(&userBatches, "batch", "b", 100, "create users in batches of N ('100' by default)")
	cmd.Flags().IntVarP(&delay, "delay", "d", 5, "the duration of the delay in between batches ('5' by default)")
	cmd.Flags().IntVarP(&resourceRate, "resource-rate", "r", 5, "every N users will have resources created to drive load on the cluster ('10' by default)")
	cmd.Flags().StringVar(&hostOperatorNamespace, "host-ns", "toolchain-host-operator", "the namespace of Host operator ('toolchain-host-operator' by default)")
	cmd.Flags().StringVar(&memberOperatorNamespace, "member-ns", "toolchain-member-operator", "the namespace of the Member operator ('toolchain-member-operator' by default)")

	if err := cmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func setup(cmd *cobra.Command, args []string) {
	cmd.SilenceUsage = true
	term := terminal.New(cmd.InOrStdin, cmd.OutOrStdout, verbose)

	term.Debugf("Number of Users:           '%d'", numberOfUsers)
	term.Debugf("User Batch Size:           '%d'", userBatches)
	term.Debugf("Load Rate:                 '%d'", resourceRate)
	term.Debugf("Host Operator Namespace:   '%s'", hostOperatorNamespace)
	term.Debugf("Member Operator Namespace: '%s'\n", memberOperatorNamespace)

	cl, config, _, err := configuration.NewClient(term, kubeconfig)
	if err != nil {
		term.Fatalf(err, "cannot create client")
	}

	// validate template file
	templateFilePath, err := filepath.Abs(args[0])
	if err != nil {
		term.Fatalf(err, "invalid filepath for template file: '%s'", args[0])
	}
	templateData, err := ioutil.ReadFile(templateFilePath)
	if err != nil {
		term.Fatalf(err, "failed to read template file: '%s'", args[0])
	}

	// validate number of users
	if numberOfUsers < 1 {
		term.Fatalf(fmt.Errorf("value must be more than 0"), "invalid users value '%d'", numberOfUsers)
	}
	if numberOfUsers%userBatches != 0 {
		term.Fatalf(fmt.Errorf("users value must be a multiple of the batch size '%d'", userBatches), "invalid users value '%d'", numberOfUsers)
	}

	term.Infof("🕖 Initializing...")
	memberClusterName, err := getMemberClusterName(cl)
	if err != nil {
		term.Fatalf(err, "unable to lookup member cluster name")
	}
	if !term.PromptBoolf("👤 provision %d users in batches of %d on %s", numberOfUsers, userBatches, config.Host) {
		return
	}

	// provision the users
	term.Infof("🍿 provisioning users...")

	uip := uiprogress.New()
	uip.Start()

	// start the progress bars in go routines
	var wg sync.WaitGroup
	usersignupBar := uip.AddBar(numberOfUsers).AppendCompleted().PrependFunc(func(b *uiprogress.Bar) string {
		return strutil.PadLeft("user signups", 15, ' ')
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for usersignupBar.Incr() {
			username := fmt.Sprintf("%s-%04d", usernamePrefix, usersignupBar.Current())
			userSignup := newUserSignup(username, memberClusterName)
			if err := cl.Create(context.TODO(), userSignup); err != nil {
				term.Fatalf(err, "failed to provision user '%s'", username)
			}
			time.Sleep(time.Millisecond * 20)

			// update Idlers timeout to kill workloads faster to reduce impact of memory/cpu usage during testing
			if err := updateUserIdlersTimeout(term, cl, username, 15*time.Second); err != nil {
				term.Fatalf(err, "failed to update idlers for user '%s'", username)
			}

			// create resources for every nth user
			if usersignupBar.Current()%resourceRate == 0 {
				userNS := fmt.Sprintf("%s-stage", username)
				if err := user.CreateResourcesFromTemplate(config, templateData, userNS); err != nil {
					term.Fatalf(err, "failed to create resources for user '%s'", username)
				}
			}

			if usersignupBar.Current()%userBatches == 0 {
				time.Sleep(time.Second * time.Duration(delay))
			}
		}
	}()
	wg.Wait()
	uip.Stop()
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
