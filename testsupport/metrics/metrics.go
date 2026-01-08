package metrics

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"k8s.io/client-go/rest"
)

func GetMetricValue(restConfig *rest.Config, baseURL string, family string, expectedLabels []string) (float64, error) {
	value, err := getMetricValue(restConfig, baseURL, family, expectedLabels, getValue)
	if value == nil {
		return -1, err
	}
	return *value, err
}

func GetHistogramBuckets(restConfig *rest.Config, baseURL string, family string, expectedLabels []string) ([]*dto.Bucket, error) {
	value, err := getMetricValue(restConfig, baseURL, family, expectedLabels, getBuckets)
	if value == nil {
		return nil, err
	}
	return *value, err
}

func getMetricValue[T any](restConfig *rest.Config, baseURL string, family string, expectedLabels []string, getValue func(dto.MetricType, *dto.Metric) (*T, error)) (*T, error) {
	if len(expectedLabels)%2 != 0 {
		return nil, fmt.Errorf("received odd number of label arguments, labels must be key-value pairs")
	}
	uri := baseURL + "/metrics"
	var metrics []byte

	client := http.Client{
		Timeout: time.Duration(30 * time.Second),
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	request, err := http.NewRequest("Get", uri, nil)
	if err != nil {
		return nil, err
	}
	if restConfig.BearerToken != "" {
		request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", restConfig.BearerToken))
	}
	resp, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	metrics, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// parse the metrics
	parser := expfmt.TextParser{}
	families, err := parser.TextToMetricFamilies(bytes.NewReader(metrics))
	if err != nil {
		return nil, err
	}
	for _, f := range families {
		if f.GetName() == family {
			metricType := f.GetType()
			// metric without labels
			if len(f.GetMetric()) == 1 && len(expectedLabels) == 0 {
				return getValue(metricType, f.GetMetric()[0])
			}

		metricSearch:
			for _, m := range f.GetMetric() {
				metricLabels := m.GetLabel()
				if len(metricLabels) != len(expectedLabels)/2 {
					continue
				}
				for i := 0; i < len(expectedLabels); {
					labelFound := false
					for _, l := range metricLabels {
						if l.GetName() == expectedLabels[i] && l.GetValue() == expectedLabels[i+1] {
							labelFound = true
						}
					}
					if !labelFound {
						continue metricSearch
					}
					i += 2
				}
				return getValue(metricType, m)
			}
		}
	}
	// here we can return `0` is the metric does not exist, which may be valid if the expected value is `0`, too.
	return new(T), fmt.Errorf("metric '%s{%v}' not found", family, expectedLabels)
}

func getValue(t dto.MetricType, m *dto.Metric) (*float64, error) {
	switch t { // nolint:exhaustive
	case dto.MetricType_COUNTER:
		value := m.GetCounter().GetValue()
		return &value, nil
	case dto.MetricType_GAUGE:
		value := m.GetGauge().GetValue()
		return &value, nil
	case dto.MetricType_UNTYPED:
		value := m.GetUntyped().GetValue()
		return &value, nil
	default:
		return nil, fmt.Errorf("unknown or unsupported metric type %s", t.String())
	}
}

func getBuckets(t dto.MetricType, m *dto.Metric) (*[]*dto.Bucket, error) {
	switch t { // nolint:exhaustive
	case dto.MetricType_HISTOGRAM:
		value := m.GetHistogram().GetBucket()
		return &value, nil
	default:
		return nil, fmt.Errorf("unknown or unsupported metric type %s", t.String())
	}
}

// GetMetricLabels return all labels (indexed by key) for all metrics of the given `family`
func GetMetricLabels(restConfig *rest.Config, baseURL string, family string) ([]map[string]string, error) {
	uri := baseURL + "/metrics"
	var metrics []byte

	client := http.Client{
		Timeout: time.Duration(30 * time.Second),
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		},
	}
	request, err := http.NewRequest("Get", uri, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Add("Authorization", fmt.Sprintf("Bearer %s", restConfig.BearerToken))
	resp, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	metrics, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// parse the metrics
	parser := expfmt.TextParser{}
	families, err := parser.TextToMetricFamilies(bytes.NewReader(metrics))
	if err != nil {
		return nil, err
	}

	labels := make([]map[string]string, 0, len(families))
	for _, f := range families {
		if f.GetName() == family {
			lbls := map[string]string{}
			labels = append(labels, lbls)
			for _, m := range f.GetMetric() {
				for _, kv := range m.GetLabel() {
					if kv.GetName() != "" {
						lbls[kv.GetName()] = kv.GetValue()
					}
				}
			}
		}
	}
	// here we can return `0` is the metric does not exist, which may be valid if the expected value is `0`, too.
	return labels, nil
}
