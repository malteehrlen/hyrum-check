package hyrumcheck_test

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"

	"hyrumcheck/hyrumcheck"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), hyrumcheck.Analyzer, "a")
}
