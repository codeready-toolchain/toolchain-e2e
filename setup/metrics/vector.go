package metrics

import (
	"context"
	"fmt"
	"time"

	prometheus "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type VectoryQuery struct {
	count int
	name  string
	query string
	sum   float64
}

func (v VectoryQuery) doQuery(apiClient prometheus.API) (float64, prometheus.Warnings, error) {
	var averageValue float64
	val, warnings, err := apiClient.Query(context.TODO(), v.query, time.Now())
	if val == nil {
		return -1, warnings, fmt.Errorf("metrics value is nil for query %s", v.name)
	}

	vector := val.(model.Vector)
	if len(vector) == 0 {
		return -1, warnings, fmt.Errorf("metrics value could not be retrieved for query %s", v.name)
	}

	for _, v := range vector {
		averageValue += float64(v.Value)
	}

	return averageValue / float64(len(vector)), warnings, err
}
