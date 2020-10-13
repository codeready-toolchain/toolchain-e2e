package wait

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"github.com/prometheus/common/expfmt"
)

func getCounter(url string, family string, labelKey string, labelValue string) (float64, error) {
	client := http.Client{
		Timeout: time.Duration(1 * time.Second),
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
	}
	resp, err := client.Get(fmt.Sprintf("https://%s/metrics", url)) // internal call, so no need for TLS
	if err != nil {
		return -1, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	metrics, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return -1, err
	}
	// parse the metrics
	parser := expfmt.TextParser{}
	families, err := parser.TextToMetricFamilies(bytes.NewReader(metrics))
	if err != nil {
		return -1, err
	}
	for _, f := range families {
		if f.GetName() == family {
			for _, m := range f.GetMetric() {
				for _, l := range m.GetLabel() {
					if l.GetName() == labelKey && l.GetValue() == labelValue {
						return m.GetCounter().GetValue(), nil
					}
				}
			}
		}
	}
	return -1, nil
}
