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

type Gatherer struct {
	client           client.Client
	interval         time.Duration
	prometheusClient prometheus.API
	queries          []*queries.Query
	term             terminal.Terminal
}

func New(term terminal.Terminal, cl client.Client, token string, interval time.Duration) Gatherer {
	url, err := getPrometheusEndpoint(cl)
	if err != nil {
		term.Fatalf(err, "error creating client: failed to get prometheus endpoint")
	}

	httpClient, err := Client(url, token)
	if err != nil {
		term.Fatalf(err, "error creating client")
	}

	promClient := prometheus.NewAPI(httpClient)

	g := Gatherer{
		client:           cl,
		interval:         interval,
		prometheusClient: promClient,
		term:             term,
	}

	g.queries = append(g.queries,
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

	return g
}

func (g *Gatherer) AddQueries(queries ...*queries.Query) {
	g.queries = append(g.queries, queries...)
}

func (g *Gatherer) Start() chan struct{} {
	if len(g.queries) == 0 {
		g.term.Infof("Metrics gatherer has no queries defined, skipping metrics gathering...")
		return nil
	}

	stop := make(chan struct{})
	go func() {
		k8sutil.Until(func() {
			for _, q := range g.queries {
				_, warnings, err := q.DoQuery(g.prometheusClient)
				if err != nil {
					if strings.Contains(err.Error(), "client error: 403") {
						url, tokenErr := auth.GetTokenRequestURI(g.client)
						if tokenErr == nil {
							g.term.Fatalf(err, "metrics query failed with 403 (Forbidden) - retrieve a new token from %s", url)
						}
					}
					g.term.Fatalf(err, "metrics query failed - check whether prometheus is still healthy in the cluster")
				} else if len(warnings) > 0 {
					g.term.Fatalf(fmt.Errorf("%v", warnings), "metrics query had unexpected warnings")
				}

			}
		}, g.interval, stop)
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

func (g *Gatherer) PrintResults() {
	for _, q := range g.queries {
		g.term.Infof("%s: %s", q.Name, q.Result())
	}
}
