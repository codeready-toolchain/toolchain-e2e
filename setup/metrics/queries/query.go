package queries

import (
	"context"
	"fmt"
	"time"

	prometheus "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type formatFunc func(float64) string

type Query struct {
	Name        string
	query       string
	SampleCount int
	Sum         float64
	format      formatFunc
}

func newQuery(name, query string, format formatFunc) *Query {
	return &Query{
		Name:   name,
		query:  query,
		format: format,
	}
}

func (q *Query) DoQuery(apiClient prometheus.API) (float64, prometheus.Warnings, error) {
	var vectorSum float64
	val, warnings, err := apiClient.Query(context.TODO(), q.query, time.Now())
	if val == nil {
		return -1, warnings, fmt.Errorf("metrics value is nil for query %s", q.Name)
	}

	vector := val.(model.Vector)
	if len(vector) == 0 {
		return -1, warnings, fmt.Errorf("metrics value could not be retrieved for query %s", q.Name)
	}

	for _, v := range vector {
		vectorSum += float64(v.Value)
	}

	datapoint := vectorSum / float64(len(vector))
	q.Sum += datapoint
	q.SampleCount++

	return datapoint, warnings, err
}

func (q *Query) Result() string {
	result := q.Sum / float64(q.SampleCount)
	return q.format(result)
}

func QueryEtcdMemoryUsage() *Query {
	return newQuery(
		"Average etcd Instance Memory Usage",
		`process_resident_memory_bytes{job="etcd"}`,
		bytesToMBString,
	)
}

func QueryClusterCPUUtilisation() *Query {
	return newQuery(
		"Average Cluster CPU Utilisation",
		`1 - avg(rate(node_cpu_seconds_total{mode="idle", cluster=""}[5m]))`,
		utilisation,
	)
}

func QueryClusterMemoryUtilisation() *Query {
	return newQuery(
		"Average Cluster Memory Utilisation",
		`1 - sum(:node_memory_MemAvailable_bytes:sum{cluster=""}) / sum(node_memory_MemTotal_bytes{cluster=""})`,
		utilisation,
	)
}

func QueryWorkloadCPUUsage(namespace, name string) *Query {
	query := fmt.Sprintf(`sum(
		node_namespace_pod_container:container_cpu_usage_seconds_total:sum_rate{cluster="", namespace="%[1]s"}
	  * on(namespace,pod)
		group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="%[1]s", workload="%[2]s", workload_type="deployment"}
	) by (pod)`, namespace, name)
	return newQuery(
		fmt.Sprintf("Average %s CPU Usage", name),
		query,
		simple,
	)
}

func QueryWorkloadMemoryUsage(namespace, name string) *Query {
	query := fmt.Sprintf(`sum(
		container_memory_working_set_bytes{cluster="", namespace="%[1]s", container!="", image!=""}
	  * on(namespace,pod)
		group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="%[1]s", workload="%[2]s", workload_type="deployment"}
	) by (pod)`, namespace, name)
	return newQuery(
		fmt.Sprintf("Average %s Memory Usage", name),
		query,
		bytesToMBString,
	)
}
