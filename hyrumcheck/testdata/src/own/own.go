package own

import (
	"errors"
	"strings"
)

func do() error { return errors.New("boom") }

// Errors from the analyzed package are not reported by default.
func f() {
	err := do()
	if strings.Contains(err.Error(), "boom") {
	}
	if err.Error() == "boom" {
	}
}
