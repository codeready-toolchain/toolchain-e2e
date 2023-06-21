package results

import (
	"encoding/csv"
	"os"

	cfg "github.com/codeready-toolchain/toolchain-e2e/setup/configuration"
	"github.com/codeready-toolchain/toolchain-e2e/setup/terminal"
)

type ResultsWriter interface {
	Write([][]string) error
	Close() error
}

type Results struct {
	stdOutWriter ResultsWriter
	csvWriter    ResultsWriter
	results      [][]string
	term         terminal.Terminal
}

func New(term terminal.Terminal) *Results {

	csvFile, err := os.Create(cfg.ResultsFilepath())
	if err != nil {
		term.Infof("failed creating file: %s", err)
		os.Exit(1)
	}

	return &Results{
		results:      make([][]string, 0),
		csvWriter:    csvWriter{csvFile},
		stdOutWriter: terminalWriter{term},
		term:         term,
	}
}

func (r *Results) writeResults() error {
	for _, w := range []ResultsWriter{r.stdOutWriter, r.csvWriter} {
		if err := w.Write(r.results); err != nil {
			return err
		}
	}
	return nil
}

func (r *Results) AddResults(results [][]string) {
	r.results = append(r.results, results...)
}

type csvWriter struct {
	f *os.File
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

// OutputResults outputs the aggregated results to the terminal and a csv file
func (r *Results) OutputResults() {
	r.writeResults()

	r.term.Infof("\nResults file: " + cfg.ResultsFilepath())
}
