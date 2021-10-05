package metrics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/codeready-toolchain/toolchain-e2e/setup/auth"
	cfg "github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/setup/metrics/queries"
	"github.com/codeready-toolchain/toolchain-e2e/setup/terminal"
	routev1 "github.com/openshift/api/route/v1"
	prometheus "github.com/prometheus/client_golang/api/prometheus/v1"

	"k8s.io/apimachinery/pkg/types"
	k8sutil "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	k8sClient        client.Client
	queryInterval    time.Duration
	prometheusClient prometheus.API
	mqueries         []queries.Query
	term             terminal.Terminal
)

func Init(t terminal.Terminal, cl client.Client, token string, interval time.Duration) {
	k8sClient = cl
	queryInterval = interval
	term = t

	url, err := getPrometheusEndpoint(cl)
	if err != nil {
		term.Fatalf(err, "error creating client: failed to get prometheus endpoint")
	}

	httpClient, err := Client(url, token)
	if err != nil {
		term.Fatalf(err, "error creating client")
	}

	prometheusClient = prometheus.NewAPI(httpClient)

	mqueries = append(mqueries,
		queries.QueryClusterCPUUtilisation(),
		queries.QueryClusterMemoryUtilisation(),
		queries.QueryEtcdMemoryUsage(),
		queries.QueryWorkloadCPUUsage(cfg.OLMOperatorNamespace, cfg.OLMOperatorWorkload),
		queries.QueryWorkloadMemoryUsage(cfg.OLMOperatorNamespace, cfg.OLMOperatorWorkload),
		queries.QueryWorkloadCPUUsage(cfg.HostOperatorNamespace, cfg.HostOperatorWorkload),
		queries.QueryWorkloadMemoryUsage(cfg.HostOperatorNamespace, cfg.HostOperatorWorkload),
		queries.QueryWorkloadCPUUsage(cfg.MemberOperatorNamespace, cfg.MemberOperatorWorkload),
		queries.QueryWorkloadMemoryUsage(cfg.MemberOperatorNamespace, cfg.MemberOperatorWorkload),
	)
}

func AddQueries(queries ...queries.Query) {
	mqueries = append(mqueries, queries...)
}

func StartGathering() chan struct{} {
	if len(mqueries) == 0 {
		term.Infof("Metrics gatherer has no queries defined, skipping metrics gathering...")
		return nil
	}

	// ensure metrics are dumped if there's a fatal error
	term.AddPreFatalExitHook(PrintResults)

	stop := make(chan struct{})
	go func() {
		k8sutil.Until(func() {
			for _, q := range mqueries {
				_, warnings, err := q.DoQuery(prometheusClient)
				if err != nil {
					if strings.Contains(err.Error(), "client error: 403") {
						url, tokenErr := auth.GetTokenRequestURI(k8sClient)
						if tokenErr == nil {
							term.Fatalf(err, "metrics query failed with 403 (Forbidden) - retrieve a new token from %s", url)
						}
					}
					term.Fatalf(err, "metrics query failed - check whether prometheus is still healthy in the cluster")
				} else if len(warnings) > 0 {
					term.Fatalf(fmt.Errorf("%v", warnings), "metrics query had unexpected warnings")
				}

			}
		}, queryInterval, stop)
	}()
	return stop
}

func getPrometheusEndpoint(client client.Client) (string, error) {
	prometheusRoute := routev1.Route{}
	if err := client.Get(context.TODO(), types.NamespacedName{
		Namespace: cfg.OpenshiftMonitoringNS,
		Name:      cfg.PrometheusRouteName,
	}, &prometheusRoute); err != nil {
		return "", err
	}
	return "https://" + prometheusRoute.Spec.Host, nil
}

func PrintResults() {
	if len(mqueries) == 0 {
		// metrics were not initialized, nothing to print
		return
	}
	for _, q := range mqueries {
		term.Infof("%s: %s", q.Name(), q.Result())
	}
}
