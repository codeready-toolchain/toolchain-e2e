package metrics

import (
	"fmt"
	"io"
	"reflect"
	"testing"

	"github.com/codeready-toolchain/toolchain-e2e/setup/metrics/queries"
	"github.com/codeready-toolchain/toolchain-e2e/setup/terminal"
	"github.com/stretchr/testify/require"

	"github.com/codeready-toolchain/toolchain-common/pkg/test"
	prometheus "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

func TestExecuteQueryAndProcessResult(t *testing.T) {
	var testTime = model.Now()

	tests := []testcase{
		{
			query: testQuery{
				name: "empty vector",
				sample: queryResult{
					val: model.Vector{},
				},
			},
			exp: expected{
				err:       "metrics value could not be retrieved for query empty vector",
				resultLen: 0,
			},
		},
		{
			query: testQuery{
				name: "first sample",
				sample: queryResult{
					val: model.Vector{
						&model.Sample{
							Metric: model.Metric{
								model.MetricNameLabel: "request_count",
							},
							Value:     40,
							Timestamp: testTime,
						},
					},
				},
			},
			exp: expected{
				err:         "",
				resultLen:   1,
				max:         40,
				sampleCount: 1,
				sum:         40,
			},
		},
		{
			query: testQuery{
				name: "second sample larger value",
				initResults: aggregateResult{
					max:         120,
					sampleCount: 1,
					sum:         120,
				},
				sample: queryResult{
					val: model.Vector{
						&model.Sample{
							Metric: model.Metric{
								model.MetricNameLabel: "request_count",
							},
							Value:     127,
							Timestamp: testTime,
						},
					},
				},
			},
			exp: expected{
				err:         "",
				resultLen:   1,
				max:         127,
				sampleCount: 2,
				sum:         247,
			},
		},
		{
			query: testQuery{
				name: "second sample smaller value",
				initResults: aggregateResult{
					max:         125,
					sampleCount: 1,
					sum:         125,
				},
				sample: queryResult{
					val: model.Vector{
						&model.Sample{
							Metric: model.Metric{
								model.MetricNameLabel: "request_count",
							},
							Value:     123,
							Timestamp: testTime,
						},
					},
				},
			},
			exp: expected{
				err:         "",
				resultLen:   1,
				max:         125,
				sampleCount: 2,
				sum:         248,
			},
		},
		{
			query: testQuery{
				name: "query error",
				sample: queryResult{
					err: fmt.Errorf("test query error"),
				},
			},
			exp: expected{
				err:       "metrics query failed - check whether prometheus is still healthy in the cluster: test query error",
				resultLen: 0,
			},
		},
		{
			query: testQuery{
				name: "query permission error",
				sample: queryResult{
					err: fmt.Errorf("failure caused by: client error: 403"),
				},
			},
			exp: expected{
				err:       "metrics query failed with 403 (Forbidden): failure caused by: client error: 403",
				resultLen: 0,
			},
		},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprintf("test %s", tc.query.name), func(t *testing.T) {
			// given
			g := &Gatherer{
				k8sClient: test.NewFakeClient(t),
				mqueries:  []queries.Query{tc.query},
				results:   map[string]aggregateResult{},
			}
			if !reflect.DeepEqual(tc.query.initResults, aggregateResult{}) {
				g.results[tc.query.name] = tc.query.initResults
			}

			// when
			err := g.sample(tc.query)

			// then
			require.Len(t, g.results, tc.exp.resultLen)
			if tc.exp.err != "" {
				require.EqualError(t, err, tc.exp.err)
			} else {
				require.NoError(t, err)
			}
			if tc.exp.resultLen > 0 {
				require.NotNil(t, g.results[tc.query.name])
				require.InDelta(t, tc.exp.max, g.results[tc.query.name].max, 0.01)
				require.InDelta(t, tc.exp.sum, g.results[tc.query.name].sum, 0.01)
				require.Equal(t, tc.exp.sampleCount, g.results[tc.query.name].sampleCount)
			}
		})
	}
}

type testcase struct {
	query testQuery
	exp   expected
}

type testQuery struct {
	name        string
	initResults aggregateResult
	sample      queryResult
}

type expected struct {
	err         string
	resultLen   int
	max         float64
	sum         float64
	sampleCount int
}

type queryResult struct {
	val  model.Value
	warn prometheus.Warnings
	err  error
}

func (q testQuery) Name() string {
	return q.name
}

func (q testQuery) Execute() (model.Value, prometheus.Warnings, error) {
	result := q.sample
	return result.val, result.warn, result.err
}

func (q testQuery) ResultType() string {
	return "memory"
}

func TestAvgZeroSamples(t *testing.T) {
	// given
	r := aggregateResult{sampleCount: 0, sum: 0, max: 0}

	// when
	result := r.avg()

	// then
	require.InDelta(t, float64(0), result, 0.01)
}

func TestComputeResultsZeroSamples(t *testing.T) {
	// given
	q := testQuery{name: "zero sample metric"}
	g := &Gatherer{
		mqueries: []queries.Query{q},
		results:  map[string]aggregateResult{"zero sample metric": {}},
		term:     &noopTerminal{},
	}

	// when
	tuples := g.ComputeResults()

	// then
	require.Len(t, tuples, 2)
	require.Equal(t, "Average zero sample metric (MB)", tuples[0][0])
	require.Equal(t, "0.00", tuples[0][1])
	require.Equal(t, "Max zero sample metric (MB)", tuples[1][0])
	require.Equal(t, "0.00", tuples[1][1])
}

type noopTerminal struct{}

var _ terminal.Terminal = &noopTerminal{}

func (t *noopTerminal) InOrStdin() io.Reader                              { return nil }
func (t *noopTerminal) OutOrStdout() io.Writer                            { return io.Discard }
func (t *noopTerminal) Debugf(msg string, args ...interface{})            {}
func (t *noopTerminal) Info(msg string)                                   {}
func (t *noopTerminal) Infof(msg string, args ...interface{})             {}
func (t *noopTerminal) Error(err error, msg string)                       {}
func (t *noopTerminal) Errorf(err error, msg string, args ...interface{}) {}
func (t *noopTerminal) Fatalf(err error, msg string, args ...interface{}) { panic("unexpected Fatalf") }
func (t *noopTerminal) Fatal(err error, msg string)                       { panic("unexpected Fatal") }
func (t *noopTerminal) PromptBoolf(msg string, args ...interface{}) bool  { return false }
func (t *noopTerminal) AddPreFatalExitHook(func())                        {}
