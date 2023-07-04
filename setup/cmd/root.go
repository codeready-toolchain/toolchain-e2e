package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/codeready-toolchain/toolchain-e2e/setup/auth"
	cfg "github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/setup/idlers"
	"github.com/codeready-toolchain/toolchain-e2e/setup/metrics"
	"github.com/codeready-toolchain/toolchain-e2e/setup/metrics/queries"
	"github.com/codeready-toolchain/toolchain-e2e/setup/operators"
	"github.com/codeready-toolchain/toolchain-e2e/setup/resources"
	"github.com/codeready-toolchain/toolchain-e2e/setup/results"
	"github.com/codeready-toolchain/toolchain-e2e/setup/terminal"
	"github.com/codeready-toolchain/toolchain-e2e/setup/users"
	"github.com/codeready-toolchain/toolchain-e2e/setup/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/gosuri/uiprogress"
	"github.com/gosuri/uitable/util/strutil"
	"github.com/spf13/cobra"
)

var (
	usernamePrefix       = "zippy"
	kubeconfig           string
	verbose              bool
	customTemplatePaths  []string
	numberOfUsers        int
	defaultTemplateUsers int
	customTemplateUsers  int
	skipAdditionalWait   bool
	skipIdlerSetup       bool
	skipInstallOperators bool
	interactive          bool
	operatorsLimit       int
	idlerTimeout         string
	token                string
	workloads            []string
)

var (
	IdlerUpdateTime         time.Duration
	DefaultApplyTimePerUser time.Duration
	CustomApplyTimePerUser  time.Duration
)

// Execute the setup command to fill a cluster with as many users as requested.
// The command uses the default `$KUBECONFIG` or `<home>/.kube/config` unless a path is specified.
func Execute() {
	cmd := &cobra.Command{
		Use:           "setup",
		Short:         "setup a cluster with the number of user accounts",
		SilenceErrors: true,
		SilenceUsage:  false,
		Args:          cobra.NoArgs,
		Run:           setup,
	}

	cmd.Flags().StringVar(&usernamePrefix, "username", usernamePrefix, "the prefix used for usersignup names")
	cmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "absolute path to the kubeconfig file")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "if 'debug' traces should be displayed in the console")
	cmd.Flags().IntVarP(&numberOfUsers, "users", "u", 2000, "the number of user accounts to provision")
	cmd.Flags().StringVar(&cfg.HostOperatorNamespace, "host-ns", cfg.DefaultHostNS, "the namespace of Host operator")
	cmd.Flags().StringVar(&cfg.MemberOperatorNamespace, "member-ns", cfg.DefaultMemberNS, "the namespace of the Member operator")
	cmd.Flags().StringSliceVar(&customTemplatePaths, "template", []string{}, "the path to the OpenShift template to apply for each custom user")
	cmd.Flags().IntVarP(&defaultTemplateUsers, cfg.DefaultTemplateUsersParam, "d", 2000, "how many users will have the default user workloads template applied")
	cmd.Flags().IntVarP(&customTemplateUsers, cfg.CustomTemplateUsersParam, "c", 2000, "how many users will have the custom user workloads template applied")
	cmd.Flags().BoolVar(&skipAdditionalWait, "skip-wait", false, "skip the additional wait time after the setup is complete to allow the cluster to settle, primarily used for debugging")
	cmd.Flags().BoolVar(&skipIdlerSetup, "skip-idler", false, "if the idler timeout should be modified for each user")
	cmd.Flags().BoolVar(&skipInstallOperators, "skip-install-operators", false, "skip the installation of operators")
	cmd.Flags().BoolVar(&interactive, "interactive", true, "if user is prompted to confirm all actions")
	cmd.Flags().IntVar(&operatorsLimit, "operators-limit", len(operators.Templates), "can be specified to limit the number of additional operators to install (by default all operators are installed to simulate cluster load in production)")
	cmd.Flags().StringVarP(&idlerTimeout, "idler-timeout", "i", "15s", "overrides the default idler timeout")
	cmd.Flags().StringVar(&cfg.Testname, "testname", "", "a name that is added as a suffix to the result file names")
	cmd.Flags().StringVarP(&token, "token", "t", "", "Openshift API token")
	cmd.Flags().StringSliceVar(&workloads, "workloads", []string{}, "workload namespace:name pairs that should have metrics collected during the setup. all values are comma-separated eg. \"--workloads service-binding-operator:service-binding-operator,rhoas-operator:rhoas-operator\"")

	if err := cmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func setup(cmd *cobra.Command, _ []string) { // nolint:gocyclo
	cmd.SilenceUsage = true
	term := terminal.New(cmd.InOrStdin, cmd.OutOrStdout, verbose)

	// call cfg.Init() to initialize variables that are dependent on any flags eg. testname
	cfg.Init(term)

	term.Infof("Number of Users:           '%d'", numberOfUsers)
	term.Infof("Default Template Users:    '%d'", defaultTemplateUsers)
	term.Infof("Custom Template Users:     '%d'", customTemplateUsers)
	term.Infof("Host Operator Namespace:   '%s'", cfg.HostOperatorNamespace)
	term.Infof("Member Operator Namespace: '%s'\n", cfg.MemberOperatorNamespace)

	generalResultsInfo := [][]string{
		{"Number of Users", strconv.Itoa(numberOfUsers)},
		{"Number of Default Template Users", strconv.Itoa(defaultTemplateUsers)},
		{"Number of Custom Template Users", strconv.Itoa(customTemplateUsers)},
	}

	// validate params
	if numberOfUsers < 1 {
		term.Fatalf(fmt.Errorf("value must be more than 0"), "invalid users value '%d'", numberOfUsers)
	}

	usersWithinBounds(term, defaultTemplateUsers, cfg.DefaultTemplateUsersParam)
	usersWithinBounds(term, customTemplateUsers, cfg.CustomTemplateUsersParam)

	if operatorsLimit > len(operators.Templates) {
		term.Fatalf(fmt.Errorf("the operators limit value must be less than or equal to '%d'", len(operators.Templates)), "invalid operators limit value '%d'", operatorsLimit)
	}

	idlerDuration, err := time.ParseDuration(idlerTimeout)
	if err != nil {
		term.Fatalf(err, "invalid idler-timeout value '%s'", idlerTimeout)
	}

	if customTemplateUsers > 0 && len(customTemplatePaths) == 0 {
		term.Fatalf(errors.New(""), "'%d' users are set to have custom templates applied but no custom templates were provided", customTemplateUsers)
	}

	for _, w := range workloads {
		pair := strings.Split(w, ":")
		if len(pair)%2 == 1 {
			term.Fatalf(err, "invalid workloads values provided '%v' - values must be namespace:name pairs", workloads)
		}
	}

	// add the default user-workloads.yaml file automatically
	defaultTemplatePath := "setup/resources/user-workloads.yaml"

	term.Infof("🕖 initializing...\n")
	cl, config, scheme, err := cfg.NewClient(term, kubeconfig)
	if err != nil {
		term.Fatalf(err, "cannot create client")
	}

	if len(token) == 0 {
		token, err = auth.GetTokenFromOC()
		if err != nil {
			tokenRequestURI, err := auth.GetTokenRequestURI(cl)
			if err != nil {
				term.Fatalf(err, "a token is required to capture metrics, use oc login to log into the cluster")
			}
			term.Fatalf(fmt.Errorf("a token can be requested from %s", tokenRequestURI), "a token is required to capture metrics, use oc login to log into the cluster. alternatively request a token and use the token flag")
		}
	}

	var templateListStr string
	templateListStr += "\n - (default) " + defaultTemplatePath
	for _, p := range customTemplatePaths {
		absPath, err := filepath.Abs(p)
		if err != nil {
			term.Fatalf(err, "invalid template file: '%s'", absPath)
		}
		_, err = os.ReadFile(absPath)
		if err != nil {
			term.Fatalf(err, "invalid template file: '%s'", absPath)
		}
		templateListStr += "\n - (custom) " + absPath
	}

	term.Infof("📋 template list: %s\n", templateListStr)
	if interactive && !term.PromptBoolf("👤 provision %d users on %s using the templates listed above", numberOfUsers, config.Host) {
		return
	}

	if err := operators.VerifySandboxOperatorsInstalled(cl); err != nil {
		term.Fatalf(err, "ensure the sandbox host and member operators are installed successfully before running the setup")
	}

	// =====================
	// begin configuration
	// =====================
	term.Infof("Configuring default space tier...")
	if err := cfg.ConfigureDefaultSpaceTier(cl); err != nil {
		term.Fatalf(err, "unable to set default space tier")
	}

	term.Infof("Disabling copied CSVs feature...")
	if err := cfg.DisableCopiedCSVs(cl); err != nil {
		term.Fatalf(err, "unable to disable OLM copy CSVs feature")
	}
	// =====================
	// end configuration
	// =====================

	// =====================
	// begin setup
	// =====================
	setupStartTime := time.Now()

	if !skipInstallOperators {
		term.Infof("⏳ installing operators...")
		// install operators for member clusters
		templatePaths := []string{}
		for i := 0; i < operatorsLimit; i++ {
			templatePaths = append(templatePaths, "setup/operators/installtemplates/"+operators.Templates[i])
		}
		if err := operators.EnsureOperatorsInstalled(cl, scheme, templatePaths); err != nil {
			term.Fatalf(err, "failed to ensure all operators are installed")
		}
	}

	// provision the users
	term.Infof("🍿 provisioning users...")

	// init the metrics gatherer
	metricsInstance := metrics.New(term, cl, token, 5*time.Minute)

	prometheusClient := metrics.GetPrometheusClient(term, cl, token)
	// add queries for each custom workload
	for _, w := range workloads {
		pair := strings.Split(w, ":")
		if len(pair) != 2 {
			term.Fatalf(err, "invalid workloads values provided '%v' - values must be namespace:name pairs", workloads)
		}
		if err := cl.Get(context.TODO(), types.NamespacedName{Namespace: pair[0], Name: pair[1]}, &appsv1.Deployment{}); err != nil {
			term.Fatalf(err, "invalid workload provided '%s'", w)
		}
		metricsInstance.AddQueries(
			queries.QueryWorkloadCPUUsage(prometheusClient, pair[0], pair[1]),
			queries.QueryWorkloadMemoryUsage(prometheusClient, pair[0], pair[1]),
		)
	}

	// redirect stdout and stderr to files due to issue with progress bars and client go logging for messages like
	// I0619 11:12:22.620509   89316 request.go:601] Waited for 1.100053529s due to client-side throttling, not priority and fairness, request: POST:https://api.rajiv.devcluster.openshift.com:6443/apis/rbac.authorization.k8s.io/v1/namespaces/waffle4-0001-dev/rolebindings
	tempStdout := os.Stdout
	tempStderr := os.Stderr
	stdOutFile, err := os.Create(cfg.StdOutFilepath())
	if err != nil {
		term.Fatalf(err, "failed creating stdout file: %s", cfg.StdOutFilepath())
	}
	stdErrFile, err := os.Create(cfg.StdErrFilepath())
	if err != nil {
		term.Fatalf(err, "failed creating stderr file: %s", cfg.StdErrFilepath())
	}
	os.Stdout = stdOutFile
	os.Stderr = stdErrFile
	term.AddPreFatalExitHook(func() {
		// restore stdout and stderr to originals
		os.Stdout = tempStdout
		os.Stderr = tempStderr
	})

	// start gathering metrics
	stopMetrics := metricsInstance.StartGathering()

	// gather and write results
	resultsWriter := results.New(term)

	outputResults := func() {
		addAndOutputResults(term, resultsWriter, func() [][]string { return generalResultsInfo }, metricsInstance.ComputeResults)
	}
	// ensure metrics are dumped even if there's a fatal error
	term.AddPreFatalExitHook(outputResults)

	uip := uiprogress.New()
	uip.Start()

	// start the progress bars and work in go routines
	var wg sync.WaitGroup

	concurrentUserSignups := 10
	usersignupBar := addProgressBar(uip, "user signups", numberOfUsers)
	signupUserFunc := func(cl client.Client, curUserNum int, username string) {
		if err := users.Create(cl, username, cfg.HostOperatorNamespace, cfg.MemberOperatorNamespace); err != nil {
			term.Fatalf(err, "failed to provision user '%s'", username)
		}

		if err := wait.ForSpace(cl, username); err != nil {
			term.Fatalf(err, "space '%s' was not ready or not found", username)
		}
	}
	userSignupRoutine := userRoutine(term, usersignupBar, signupUserFunc)
	splitToMultipleRoutines(&wg, concurrentUserSignups, userSignupRoutine)

	var idlerBar *userProgressBar
	if !skipIdlerSetup {
		concurrentIdlerSetups := 3
		idlerBar = addProgressBar(uip, "idler setup", numberOfUsers)
		updateIdlerFunc := func(cl client.Client, curUserNum int, username string) {
			// update Idlers timeout to kill workloads faster to reduce impact of memory/cpu usage during testing
			if err := idlers.UpdateTimeout(cl, username, idlerDuration); err != nil {
				term.Fatalf(err, "failed to update idlers for user '%s'", username)
			}
		}
		ur := userRoutine(term, idlerBar, updateIdlerFunc)
		splitToMultipleRoutines(&wg, concurrentIdlerSetups, ur)
	}

	var defaultUserSetupBar *userProgressBar
	concurrentUserSetups := 5
	if defaultTemplateUsers > 0 {
		defaultUserSetupBar = addProgressBar(uip, "setup default template users", defaultTemplateUsers)
		setupDefaultUsersFunc := func(cl client.Client, curUserNum int, username string) {
			if curUserNum <= defaultTemplateUsers {
				if err := resources.CreateUserResourcesFromTemplateFiles(cl, scheme, username, []string{defaultTemplatePath}); err != nil {
					term.Fatalf(err, "failed to create default template resources for user '%s'", username)
				}
			}
		}
		ur := userRoutine(term, defaultUserSetupBar, setupDefaultUsersFunc)
		splitToMultipleRoutines(&wg, concurrentUserSetups, ur)
	}

	var customUserSetupBar *userProgressBar
	if customTemplateUsers > 0 && len(customTemplatePaths) > 0 {
		customUserSetupBar = addProgressBar(uip, "setup custom template users", customTemplateUsers)
		setupCustomUsersFunc := func(cl client.Client, curUserNum int, username string) {
			if curUserNum <= customTemplateUsers {
				if err := resources.CreateUserResourcesFromTemplateFiles(cl, scheme, username, customTemplatePaths); err != nil {
					term.Fatalf(err, "failed to create custom template resources for user '%s'", username)
				}
			}
		}
		ur := userRoutine(term, customUserSetupBar, setupCustomUsersFunc)
		splitToMultipleRoutines(&wg, concurrentUserSetups, ur)
	}

	defer close(stopMetrics)
	wg.Wait()
	uip.Stop()

	// restore stdout and stderr to originals
	os.Stdout = tempStdout
	os.Stderr = tempStderr

	term.Infof("🏁 done provisioning users")

	// continue gathering metrics for some time after creating all users and resources since memory usage was observed to continue changing
	if !skipAdditionalWait {
		additionalMetricsDuration := 15 * time.Minute
		term.Infof("Continuing to gather metrics for %s...", additionalMetricsDuration)
		time.Sleep(additionalMetricsDuration)
	}

	// =====================
	// end of setup
	// =====================

	totalRunningTime := time.Since(setupStartTime)
	if idlerBar != nil {
		IdlerUpdateTime = idlerBar.timeSpent
	}
	if defaultUserSetupBar != nil {
		DefaultApplyTimePerUser = defaultUserSetupBar.timeSpent
	}
	if customUserSetupBar != nil {
		CustomApplyTimePerUser = customUserSetupBar.timeSpent
	}

	generalResultsInfo = append(generalResultsInfo,
		[]string{"Average Idler Update Time (s)", fmt.Sprintf("%.2f", IdlerUpdateTime.Seconds()/float64(numberOfUsers))},
		[]string{"Average Time Per User - default (s)", fmt.Sprintf("%.2f", DefaultApplyTimePerUser.Seconds()/float64(numberOfUsers))},
		[]string{"Average Time Per User - custom (s)", fmt.Sprintf("%.2f", CustomApplyTimePerUser.Seconds()/float64(numberOfUsers))},
		[]string{"Total Running Time (m)", fmt.Sprintf("%f", totalRunningTime.Minutes())},
	)

	outputResults()
	term.Infof("👋 have fun!")
}

func usersWithinBounds(term terminal.Terminal, value int, templateType string) {
	if value < 0 || value > numberOfUsers {
		term.Fatalf(fmt.Errorf("value must be between 0 and %d", numberOfUsers), "invalid '%s' users value '%d'", templateType, value)
	}
}

func addAndOutputResults(term terminal.Terminal, resultsWriter *results.Results, r ...func() [][]string) {
	// add header row
	resultsWriter.AddResults([][]string{
		{"Item", "Value"},
	})
	// add results
	for _, result := range r {
		resultsWriter.AddResults(result())
	}

	term.Infof("\n📈 Results 📉")
	resultsWriter.OutputResults()
}

type userProgressBar struct {
	mu        sync.Mutex
	timeSpent time.Duration
	bar       *uiprogress.Bar
}

func addProgressBar(uip *uiprogress.Progress, description string, total int) *userProgressBar {

	bar := uip.AddBar(total).AppendCompleted().PrependFunc(func(b *uiprogress.Bar) string {
		return strutil.PadLeft(fmt.Sprintf("%s (%d/%d)", description, b.Current(), total), 40, ' ')
	})

	return &userProgressBar{
		bar: bar,
	}
}

func (b *userProgressBar) Incr() (bool, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.bar.Incr(), b.bar.Current()
}

func (b *userProgressBar) AddTimeSpent(d time.Duration) {
	b.mu.Lock()
	b.timeSpent += d
	b.mu.Unlock()
}

func splitToMultipleRoutines(parent *sync.WaitGroup, concurrentRoutinesCount int, routine func(*sync.WaitGroup)) {
	parent.Add(1)
	go func() {
		defer parent.Done()
		var subgroup sync.WaitGroup
		subgroup.Add(concurrentRoutinesCount)
		for i := 0; i < concurrentRoutinesCount; i++ {
			go routine(&subgroup)
		}
		subgroup.Wait()
	}()
}

func userRoutine(term terminal.Terminal, progressBar *userProgressBar, ua userAction) func(wg *sync.WaitGroup) {
	return func(subgroup *sync.WaitGroup) {
		aCl, _, _, err := cfg.NewClient(term, kubeconfig)
		if err != nil {
			term.Fatalf(err, "cannot create client")
		}

		hasMore, curUserNum := progressBar.Incr()
		for hasMore {
			username := fmt.Sprintf("%s-%04d", usernamePrefix, curUserNum)

			startTime := time.Now()

			ua(aCl, curUserNum, username)

			timeSpent := time.Since(startTime)
			progressBar.AddTimeSpent(timeSpent)
			hasMore, curUserNum = progressBar.Incr()
		}
		subgroup.Done()
	}
}

type userAction func(cl client.Client, curUserNum int, username string)
