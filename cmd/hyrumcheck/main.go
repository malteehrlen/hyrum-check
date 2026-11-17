// Command hyrumcheck reports code that depends on error message text.
package main

import (
	"hyrumcheck/hyrumcheck"

	"golang.org/x/tools/go/analysis/singlechecker"
)

func main() { singlechecker.Main(hyrumcheck.Analyzer) }
