package queries

import (
	"context"
	"fmt"
	"time"

	prometheus "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type Query interface {
	Name() string
	DoQuery(apiClient prometheus.API) (float64, prometheus.Warnings, error)
	Result() string
}

type baseQuery struct {
	name        string
	query       string
	sampleCount int
}

func (b baseQuery) Name() string {
	return b.name
}

func (q *baseQuery) DoQuery(apiClient prometheus.API) (float64, prometheus.Warnings, error) {
	var vectorSum float64
	val, warnings, err := apiClient.Query(context.TODO(), q.query, time.Now())
	if val == nil {
		return -1, warnings, fmt.Errorf("metrics value is nil for query %s", q.Name())
	}

	vector := val.(model.Vector)
	if len(vector) == 0 {
		return -1, warnings, fmt.Errorf("metrics value could not be retrieved for query %s", q.Name())
	}

	for _, v := range vector {
		vectorSum += float64(v.Value)
	}

	datapoint := vectorSum / float64(len(vector))

	return datapoint, warnings, err
}

type avgUtilizationQuery struct {
	baseQuery
	sum float64
}

func (q *avgUtilizationQuery) DoQuery(apiClient prometheus.API) (float64, prometheus.Warnings, error) {
	datapoint, warnings, err := q.baseQuery.DoQuery(apiClient)
	q.sum += datapoint
	q.sampleCount++
	return datapoint, warnings, err
}

func (q *avgUtilizationQuery) Result() string {
	result := q.sum / float64(q.sampleCount)
	return percentage(result)
}

type memoryQuery struct {
	baseQuery
	lastValue float64
}

func (q *memoryQuery) DoQuery(apiClient prometheus.API) (float64, prometheus.Warnings, error) {
	datapoint, warnings, err := q.baseQuery.DoQuery(apiClient)
	q.lastValue = datapoint
	q.sampleCount++

	return datapoint, warnings, err
}

func (q *memoryQuery) Result() string {
	return bytesToMBString(q.lastValue)
}

type avgCPUQuery struct {
	baseQuery
	sum float64
}

func (q *avgCPUQuery) DoQuery(apiClient prometheus.API) (float64, prometheus.Warnings, error) {
	datapoint, warnings, err := q.baseQuery.DoQuery(apiClient)
	q.sum += datapoint
	q.sampleCount++

	return datapoint, warnings, err
}

func (q *avgCPUQuery) Result() string {
	result := q.sum / float64(q.sampleCount)
	return simple(result)
}

func QueryEtcdMemoryUsage() *memoryQuery {
	return &memoryQuery{
		baseQuery: baseQuery{
			name:  "Average etcd Instance Memory Usage",
			query: `process_resident_memory_bytes{job="etcd"}`,
		},
	}
}

func QueryClusterCPUUtilisation() *avgUtilizationQuery {
	return &avgUtilizationQuery{
		baseQuery: baseQuery{
			name:  "Average Cluster CPU Utilisation",
			query: `1 - avg(rate(node_cpu_seconds_total{mode="idle", cluster=""}[5m]))`,
		},
	}
}

func QueryClusterMemoryUtilisation() *avgUtilizationQuery {
	return &avgUtilizationQuery{
		baseQuery: baseQuery{
			name:  "Average Cluster Memory Utilisation",
			query: `1 - sum(:node_memory_MemAvailable_bytes:sum{cluster=""}) / sum(node_memory_MemTotal_bytes{cluster=""})`,
		},
	}
}

func QueryWorkloadCPUUsage(namespace, name string) *avgCPUQuery {
	query := fmt.Sprintf(`sum(
		node_namespace_pod_container:container_cpu_usage_seconds_total:sum_rate{cluster="", namespace="%[1]s"}
	  * on(namespace,pod)
		group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="%[1]s", workload="%[2]s", workload_type="deployment"}
	) by (pod)`, namespace, name)
	return &avgCPUQuery{
		baseQuery: baseQuery{
			name:  fmt.Sprintf("Average %s CPU Usage", name),
			query: query,
		},
	}
}

func QueryWorkloadMemoryUsage(namespace, name string) *memoryQuery {
	query := fmt.Sprintf(`sum(
		container_memory_working_set_bytes{cluster="", namespace="%[1]s", container!="", image!=""}
	  * on(namespace,pod)
		group_left(workload, workload_type) namespace_workload_pod:kube_pod_owner:relabel{cluster="", namespace="%[1]s", workload="%[2]s", workload_type="deployment"}
	) by (pod)`, namespace, name)
	return &memoryQuery{
		baseQuery: baseQuery{
			name:  fmt.Sprintf("Average %s Memory Usage", name),
			query: query,
		},
	}
}
