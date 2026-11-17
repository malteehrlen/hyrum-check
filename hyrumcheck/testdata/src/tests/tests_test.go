package tests

import "testing"

func TestX(t *testing.T) {
	var err error
	if err.Error() == "x" { // want `comparing error message text`
	}
}
