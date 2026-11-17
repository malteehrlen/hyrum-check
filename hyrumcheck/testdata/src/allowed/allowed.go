package allowed

import (
	"ext"
	"strings"
)

func f() {
	_, err := ext.Legacy()
	if strings.Contains(err.Error(), "x") {
	}
	_, err = ext.Get()
	if strings.Contains(err.Error(), "x") { // want `error originates in ext`
	}
}
