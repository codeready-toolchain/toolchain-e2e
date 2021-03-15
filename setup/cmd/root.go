package cmd

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	toolchainv1alpha1 "github.com/codeready-toolchain/api/pkg/apis/toolchain/v1alpha1"
	cfg "github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/setup/terminal"
	"github.com/codeready-toolchain/toolchain-e2e/setup/user"
	"github.com/codeready-toolchain/toolchain-e2e/testsupport/md5"

	"github.com/gosuri/uiprogress"
	"github.com/gosuri/uitable/util/strutil"
	"github.com/spf13/cobra"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var usernamePrefix = "zippy"
var kubeconfig string
var verbose bool
var hostOperatorNamespace string
var memberOperatorNamespace string
var templatePath string
var numberOfUsers int
var userBatches int
var resourceRate int
var resourceProcessorsCount int

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

	quayNS, found := os.LookupEnv("QUAY_NAMESPACE")
	if !found || len(quayNS) == 0 {
		fmt.Println("QUAY_NAMESPACE env var is not set, ensure the prerequisite setup steps are followed")
		os.Exit(1)
	}
	defaultHostNS := fmt.Sprintf("%s-host-operator", quayNS)
	defaultMemberNS := fmt.Sprintf("%s-member-operator", quayNS)

	cmd.Flags().StringVar(&usernamePrefix, "username", usernamePrefix, "the prefix used for usersignup names")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "(optional) absolute path to the kubeconfig file")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "if 'debug' traces should be displayed in the console (false by default)")
	cmd.Flags().IntVarP(&numberOfUsers, "users", "u", 3000, "provision N users ('3000' by default)")
	cmd.Flags().IntVarP(&userBatches, "batch", "b", 25, "create users in batches of N, increasing batch size may cause performance problems ('25' by default)")
	cmd.Flags().IntVarP(&resourceRate, "resource-rate", "r", 5, "every N users will have resources created to drive load on the cluster ('10' by default)")
	cmd.Flags().StringVar(&hostOperatorNamespace, "host-ns", defaultHostNS, "the namespace of Host operator ('${QUAY_NAMESPACE}-host-operator' by default)")
	cmd.Flags().StringVar(&memberOperatorNamespace, "member-ns", defaultMemberNS, "the namespace of the Member operator ('${QUAY_NAMESPACE}-member-operator' by default)")
	cmd.Flags().IntVar(&resourceProcessorsCount, "resource-processors", 20, "the number of resource processors used for creating user resources, increase value to process templates with more resources faster ('20' by default)")

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

	cl, config, _, err := cfg.NewClient(term, kubeconfig)
	if err != nil {
		term.Fatalf(err, "cannot create client")
	}

	s := user.Setup{
		Config: config,
		Client: cl,
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

	// validate concurrent creates
	if resourceProcessorsCount < 1 {
		term.Fatalf(fmt.Errorf("value must be more than 0"), "invalid resource processors value '%d'", resourceProcessorsCount)
	}

	term.Infof("🕖 initializing...")
	memberClusterName, err := s.GetMemberClusterName(hostOperatorNamespace, memberOperatorNamespace)
	if err != nil {
		term.Fatalf(err, "unable to lookup member cluster name, ensure the sandbox setup steps are followed")
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
		return strutil.PadLeft(fmt.Sprintf("user signups (%d/%d)", b.Current(), numberOfUsers), 25, ' ')
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for usersignupBar.Incr() {
			username := fmt.Sprintf("%s-%04d", usernamePrefix, usersignupBar.Current())
			userSignup := newUserSignup(username, memberClusterName)
			if err := s.Client.Create(context.TODO(), userSignup); err != nil {
				term.Fatalf(err, "failed to provision user '%s'", username)
			}
			time.Sleep(time.Millisecond * 20)

			// when the batch is done, wait for the user's namespaces to exist before proceeding
			if usersignupBar.Current()%userBatches == 0 {
				for i := usersignupBar.Current() - userBatches + 1; i < usersignupBar.Current(); i++ {
					userToCheck := fmt.Sprintf("%s-%04d", usernamePrefix, i)
					userNS := fmt.Sprintf("%s-stage", userToCheck)
					if err := s.WaitForNamespace(userNS); err != nil {
						term.Fatalf(err, "failed to find namespace '%s'", userNS)
					}
				}
			}
		}
	}()

	setupBar := uip.AddBar(numberOfUsers).AppendCompleted().PrependFunc(func(b *uiprogress.Bar) string {
		return strutil.PadLeft(fmt.Sprintf("user setup (%d/%d)", b.Current(), numberOfUsers), 25, ' ')
	})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for setupBar.Incr() {
			username := fmt.Sprintf("%s-%04d", usernamePrefix, setupBar.Current())
			userNS := fmt.Sprintf("%s-stage", username)
			// waiting for each namespace here prevents some edge cases where the setup job can progress beyond the usersignup job and fail with a timeout
			if err := s.WaitForNamespace(userNS); err != nil {
				term.Fatalf(err, "failed to find namespace '%s'", userNS)
			}

			// update Idlers timeout to kill workloads faster to reduce impact of memory/cpu usage during testing
			if err := s.UpdateUserIdlersTimeout(username, 15*time.Second); err != nil {
				term.Fatalf(err, "failed to update idlers for user '%s'", username)
			}

			// create resources for every nth user
			if setupBar.Current()%resourceRate == 0 {
				if err := user.CreateResourcesFromTemplate(s.Config, userNS, templateData, resourceProcessorsCount); err != nil {
					term.Fatalf(err, "failed to create resources for user '%s'", username)
				}
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
