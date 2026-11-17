package hyrumcheck_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"github.com/malteehrlen/hyrum-check/hyrumcheck"
)

func setFlag(t *testing.T, name, value string) {
	t.Helper()
	old := hyrumcheck.Analyzer.Flags.Lookup(name).Value.String()
	if err := hyrumcheck.Analyzer.Flags.Set(name, value); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hyrumcheck.Analyzer.Flags.Set(name, old) })
}

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), hyrumcheck.Analyzer, "a", "own")
}

func TestInternal(t *testing.T) {
	setFlag(t, "internal", "true")
	analysistest.Run(t, analysistest.TestData(), hyrumcheck.Analyzer, "owni")
}

func TestAllow(t *testing.T) {
	setFlag(t, "allow", "ext.Legacy")
	analysistest.Run(t, analysistest.TestData(), hyrumcheck.Analyzer, "allowed")
}

func TestTestsFlag(t *testing.T) {
	setFlag(t, "tests", "true")
	analysistest.Run(t, analysistest.TestData(), hyrumcheck.Analyzer, "tests")
}
