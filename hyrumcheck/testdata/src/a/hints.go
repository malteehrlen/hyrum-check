package a

import (
	"ext"
	"noerrs"
	"os"
	"strings"
)

func hints() {
	_, err := ext.Get()
	if strings.Contains(err.Error(), "x") { // want `error originates in ext; try errors.Is with ext.ErrNotFound or errors.As with \*ext.Error`
	}
	err = noerrs.Do()
	if err.Error() == "x" { // want `noerrs, which exports no sentinel or typed errors`
	}
	_, err = os.Open("x")
	if err.Error() == "x" { // want `error originates in os; try errors.Is with os.ErrClosed`
	}
}
