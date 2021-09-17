package metrics

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/codeready-toolchain/toolchain-e2e/setup/auth"
	cfg "github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/setup/terminal"
	routev1 "github.com/openshift/api/route/v1"
	prometheus "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"k8s.io/apimachinery/pkg/types"
	k8sutil "k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Gatherer struct {
	client   client.Client
	interval time.Duration
	queries  []*QueryDef
	term     terminal.Terminal
	token    string
}

type QueryFn func(apiClient prometheus.API) (float64, prometheus.Warnings, error)

type QueryDef struct {
	count int
	name  string
	query QueryFn
	sum   float64
}

func New(term terminal.Terminal, cl client.Client, token string, interval time.Duration) Gatherer {
	g := Gatherer{
		client:   cl,
		interval: interval,
		term:     term,
		token:    token,
	}

	g.AddQueries(
		QueryClusterCPUUtilisation(),
		QueryClusterMemoryUtilisation(),
		QueryWorkloadCPUUtilisation(cfg.HostOperatorNamespace, cfg.HostOperatorWorkload),
		QueryWorkloadMemoryUtilisation(cfg.HostOperatorNamespace, cfg.HostOperatorWorkload),
		QueryWorkloadCPUUtilisation(cfg.MemberOperatorNamespace, cfg.MemberOperatorWorkload),
		QueryWorkloadMemoryUtilisation(cfg.MemberOperatorNamespace, cfg.MemberOperatorWorkload),
	)

	return g
}

func (g *Gatherer) Start() chan struct{} {
	url, err := g.getPrometheusEndpoint()
	if err != nil {
		g.term.Fatalf(err, "error creating client: failed to get prometheus endpoint")
	}

	client, err := Client(url, g.token)
	if err != nil {
		g.term.Fatalf(err, "error creating client")
	}
	if len(g.queries) == 0 {
		return nil
	}
	apiClient := prometheus.NewAPI(client)
	stop := make(chan struct{})
	go func() {
		k8sutil.Until(func() {
			for _, q := range g.queries {
				val, warnings, err := q.query(apiClient)
				if err != nil {
					if strings.Contains(err.Error(), "client error: 403") {
						url, tokenErr := auth.GetTokenRequestURI(g.client)
						if tokenErr == nil {
							g.term.Fatalf(err, "metrics query failed with 403 (Forbidden) - retrieve a new token from %s", url)
						}
					}
					g.term.Fatalf(err, "metrics query failed")
				} else if len(warnings) > 0 {
					g.term.Fatalf(fmt.Errorf("%v", warnings), "metrics query had unexpected warnings")
				}

				q.sum += val
				q.count++
			}
		}, g.interval, stop)
	}()
	return stop
}

func (g *Gatherer) AddQueries(queries ...*QueryDef) {
	g.queries = append(g.queries, queries...)
}

func QueryClusterCPUUtilisation() *QueryDef {
	query := `1 - avg(rate(node_cpu_seconds_total{mode="idle", cluster=""}[5m]))`
	fn := func(apiClient prometheus.API) (float64, prometheus.Warnings, error) {
		return vectorQuery(apiClient, query)
	}
	return &QueryDef{name: "Cluster CPU Utilisation", query: fn}
}

func QueryClusterMemoryUtilisation() *QueryDef {
	query := `1 - sum(:node_memory_MemAvailable_bytes:sum{cluster=""}) / sum(node_memory_MemTotal_bytes{cluster=""})`
	fn := func(apiClient prometheus.API) (float64, prometheus.Warnings, error) {
		return vectorQuery(apiClient, query)
	}
	return &QueryDef{name: "Cluster Memory Utilisation", query: fn}
}

func QueryWorkloadCPUUtilisation(namespace, name string) *QueryDef {
	query := fmt.Sprintf(`sum(
		node_namespace_pod_container:container_cpu_usage_seconds_total:sum_rate{cluster="", namespace="%[1]s"}
	  * on(namespace,pod)
		group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="%[1]s", workload="%[2]s", workload_type="deployment"}
	) by (pod)
	/sum(
		kube_pod_container_resource_requests{cluster="", namespace="%[1]s", resource="cpu"}
	  * on(namespace,pod) 
		group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="%[1]s", workload="%[2]s", workload_type="deployment"}
	) by (pod)`, namespace, name)
	fn := func(apiClient prometheus.API) (float64, prometheus.Warnings, error) {
		return vectorQuery(apiClient, query)
	}
	return &QueryDef{name: fmt.Sprintf("%s CPU Utilisation", name), query: fn}
}

func QueryWorkloadMemoryUtilisation(namespace, name string) *QueryDef {
	query := fmt.Sprintf(`sum(
		container_memory_working_set_bytes{cluster="", namespace="%[1]s", container!="", image!=""}
	  * on(namespace,pod)
		group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="%[1]s", workload="%[2]s", workload_type="deployment"}
	) by (pod)
	/sum(
		kube_pod_container_resource_requests{cluster="", namespace="%[1]s", resource="memory"}
	  * on(namespace,pod)
		group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="%[1]s", workload="%[2]s", workload_type="deployment"}
	) by (pod)`, namespace, name)
	fn := func(apiClient prometheus.API) (float64, prometheus.Warnings, error) {
		return vectorQuery(apiClient, query)
	}
	return &QueryDef{name: fmt.Sprintf("%s Memory Utilisation", name), query: fn}
}

func (g *Gatherer) getPrometheusEndpoint() (string, error) {
	prometheusRoute := routev1.Route{}
	if err := g.client.Get(context.TODO(), types.NamespacedName{
		Namespace: cfg.OpenshiftMonitoringNS,
		Name:      cfg.PrometheusRouteName,
	}, &prometheusRoute); err != nil {
		return "", err
	}
	return "https://" + prometheusRoute.Spec.Host, nil
}

func (g *Gatherer) PrintResults() {
	for _, q := range g.queries {
		g.term.Infof("Average %s: %f", q.name, q.sum/float64(q.count))
	}
}

func vectorQuery(apiClient prometheus.API, query string) (float64, prometheus.Warnings, error) {
	var averageValue float64
	val, warnings, err := apiClient.Query(context.TODO(), query, time.Now())
	vector := val.(model.Vector)
	if len(vector) == 0 {
		return -1, warnings, fmt.Errorf("metrics value could not be retrieved")
	}
	for _, v := range vector {
		averageValue += float64(v.Value)
	}
	averageValue /= float64(len(vector))

	return averageValue, warnings, err
}
