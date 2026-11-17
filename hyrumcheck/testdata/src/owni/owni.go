package owni

import "errors"

func do() error { return errors.New("boom") }

func f() {
	err := do()
	if err.Error() == "boom" { // want `comparing error message text; use errors.Is or errors.As instead`
	}
}
