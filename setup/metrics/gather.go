package metrics

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"strings"
	"time"

	"github.com/codeready-toolchain/toolchain-e2e/setup/auth"
	cfg "github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/setup/metrics/queries"
	"github.com/codeready-toolchain/toolchain-e2e/setup/terminal"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	k8sutil "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	OpenshiftMonitoringNS = "openshift-monitoring"
	PrometheusRouteName   = "prometheus-k8s"

	OLMOperatorNamespace = "openshift-operator-lifecycle-manager"
	OLMOperatorWorkload  = "olm-operator"

	OSAPIServerNamespace = "openshift-apiserver"
	OSAPIServerWorkload  = "apiserver"
)

type Gatherer struct {
	k8sClient     client.Client
	queryInterval time.Duration
	mqueries      []queries.Query
	results       map[string]aggregateResult
	otherResults  [][]string
	term          terminal.Terminal
}

type aggregateResult struct {
	sampleCount int
	max         float64
	sum         float64
}

func (r aggregateResult) avg() float64 {
	return r.sum / float64(r.sampleCount)
}

// New creates a new gatherer with default queries
func New(t terminal.Terminal, cl client.Client, token string, interval time.Duration) *Gatherer {
	g := &Gatherer{
		k8sClient:     cl,
		queryInterval: interval,
		term:          t,
	}

	prometheusClient := GetPrometheusClient(t, cl, token)

	// Add default queries
	g.AddQueries(
		queries.QueryClusterCPUUtilisation(prometheusClient),
		queries.QueryClusterMemoryUtilisation(prometheusClient),
		queries.QueryNodeMemoryUtilisation(prometheusClient),
		queries.QueryEtcdMemoryUsage(prometheusClient),
		queries.QueryWorkloadCPUUsage(prometheusClient, OLMOperatorNamespace, OLMOperatorWorkload),
		queries.QueryWorkloadMemoryUsage(prometheusClient, OLMOperatorNamespace, OLMOperatorWorkload),
		queries.QueryOpenshiftKubeAPIMemoryUtilisation(prometheusClient),
		queries.QueryWorkloadCPUUsage(prometheusClient, OSAPIServerNamespace, OSAPIServerWorkload),
		queries.QueryWorkloadMemoryUsage(prometheusClient, OSAPIServerNamespace, OSAPIServerWorkload),
		queries.QueryWorkloadCPUUsage(prometheusClient, cfg.HostOperatorNamespace, cfg.HostOperatorWorkload),
		queries.QueryWorkloadMemoryUsage(prometheusClient, cfg.HostOperatorNamespace, cfg.HostOperatorWorkload),
		queries.QueryWorkloadCPUUsage(prometheusClient, cfg.MemberOperatorNamespace, cfg.MemberOperatorWorkload),
		queries.QueryWorkloadMemoryUsage(prometheusClient, cfg.MemberOperatorNamespace, cfg.MemberOperatorWorkload),
	)
	g.results = make(map[string]aggregateResult, len(g.mqueries))

	return g
}

// nolint
func NewEmpty(t terminal.Terminal, cl client.Client, interval time.Duration) *Gatherer {
	g := &Gatherer{
		k8sClient:     cl,
		queryInterval: interval,
		term:          t,
	}
	g.results = make(map[string]aggregateResult, len(g.mqueries))
	return g
}

func (g *Gatherer) AddQueries(queries ...queries.Query) {
	g.mqueries = append(g.mqueries, queries...)
}

func (g *Gatherer) StartGathering() chan struct{} {
	if len(g.mqueries) == 0 {
		g.term.Infof("Metrics gatherer has no queries defined, skipping metrics gathering...")
		return nil
	}

	// ensure metrics are dumped if there's a fatal error
	g.term.AddPreFatalExitHook(g.OutputResults)

	stop := make(chan struct{})
	go func() {
		k8sutil.Until(func() {
			for _, q := range g.mqueries {
				err := g.sample(q)
				if err != nil {
					g.term.Fatalf(err, "metrics error")
				}
			}
		}, g.queryInterval, stop)
	}()
	return stop
}

func (g *Gatherer) sample(q queries.Query) error {
	val, warnings, err := q.Execute()
	if err != nil {
		if strings.Contains(err.Error(), "client error: 403") {
			url, tokenErr := auth.GetTokenRequestURI(g.k8sClient)
			if tokenErr != nil {
				return errors.Wrapf(err, "metrics query failed with 403 (Forbidden)")
			}
			return errors.Wrapf(err, "metrics query failed with 403 (Forbidden) - retrieve a new token from %s", url)
		}
		return errors.Wrapf(err, "metrics query failed - check whether prometheus is still healthy in the cluster")
	} else if len(warnings) > 0 {
		return errors.Wrapf(fmt.Errorf("warnings: %v", warnings), "metrics query had unexpected warnings")
	}

	vector := val.(model.Vector)
	if len(vector) == 0 {
		return fmt.Errorf("metrics value could not be retrieved for query %s", q.Name())
	}

	// if a result returns multiple vector samples we'll take the average of the values to get a single datapoint for the sake of simplicity
	var vectorSum float64
	for _, v := range vector {
		vectorSum += float64(v.Value)
	}
	datapoint := vectorSum / float64(len(vector))

	r := g.results[q.Name()]
	r.max = math.Max(r.max, datapoint)
	r.sum += datapoint
	r.sampleCount++
	g.results[q.Name()] = r
	return nil
}

// OutputResults outputs the aggregated results to the terminal and a csv file
func (g *Gatherer) OutputResults() {
	pwd, err := os.Getwd()
	if err != nil {
		g.term.Infof("error getting current working directory %s", err)
		os.Exit(1)
	}
	resultsDir := pwd + "/results/"
	os.MkdirAll(resultsDir, os.ModePerm)
	resultsFilepath := resultsDir + time.Now().Format("2006-01-02_15:04:05") + ".csv"

	csvWriter := g.newCSVWriter(resultsFilepath)

	g.writeResults(terminalWriter{g.term}, csvWriter)

	g.term.Infof("\nResults file: " + csvWriter.path)
}

// Results iterates through each query and aggregates the results
func (g *Gatherer) computeResults() [][]string {
	var tuples [][]string
	for _, q := range g.mqueries {
		result := g.results[q.Name()]
		switch q.ResultType() {
		case "percentage":
			tuples = append(tuples, []string{fmt.Sprintf("Average %s (%%)", q.Name()), percentage(result.avg())})
			tuples = append(tuples, []string{fmt.Sprintf("Max %s (%%)", q.Name()), percentage(result.max)})
		case "memory":
			tuples = append(tuples, []string{fmt.Sprintf("Average %s (MB)", q.Name()), bytesToMBString(result.avg())})
			tuples = append(tuples, []string{fmt.Sprintf("Max %s (MB)", q.Name()), bytesToMBString(result.max)})
		case "simple":
			tuples = append(tuples, []string{fmt.Sprintf("Average %s", q.Name()), bytesToMBString(result.avg())})
			tuples = append(tuples, []string{fmt.Sprintf("Max %s", q.Name()), bytesToMBString(result.max)})
		default:
			g.term.Fatalf(fmt.Errorf("query %s is missing a result type", q.Name()), "invalid query")
		}
	}
	return tuples
}

func (g *Gatherer) AddResults(otherResults [][]string) {
	g.otherResults = append(g.otherResults, otherResults...)
}

type resultsWriter interface {
	Write([][]string) error
	Close() error
}

func (g *Gatherer) writeResults(writers ...resultsWriter) error {
	results := [][]string{}
	results = append(results, []string{"Item", "Value"})
	results = append(results, g.computeResults()...)
	results = append(results, g.otherResults...)
	for _, w := range writers {
		if err := w.Write(results); err != nil {
			return err
		}
	}
	return nil
}

func (g *Gatherer) newCSVWriter(resultsFilepath string) *csvWriter {
	csvFile, err := os.Create(resultsFilepath)
	if err != nil {
		g.term.Infof("failed creating file: %s", err)
		os.Exit(1)
	}

	return &csvWriter{resultsFilepath, csvFile}
}

type csvWriter struct {
	path string
	f    *os.File
}

func (w csvWriter) Write(results [][]string) error {
	writer := csv.NewWriter(w.f)
	return writer.WriteAll(results)
}

func (w csvWriter) Close() error {
	return w.f.Close()
}

type terminalWriter struct {
	t terminal.Terminal
}

func (w terminalWriter) Write(results [][]string) error {
	for _, result := range results {
		w.t.Infof("%s: %s", result[0], result[1])
	}
	return nil
}

func (w terminalWriter) Close() error {
	return nil
}
