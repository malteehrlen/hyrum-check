package a

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

var re = regexp.MustCompile(`timeout`)

func bad(err error) {
	if strings.Contains(err.Error(), "not found") { // want `strings.Contains depends on error message text`
	}
	if err.Error() == "EOF" { // want `comparing error message text`
	}
	if "EOF" != err.Error() { // want `comparing error message text`
	}
	switch err.Error() { // want `switching on error message text`
	case "a":
	}
	msg := err.Error()
	if strings.HasPrefix(msg, "x") { // want `strings.HasPrefix depends on error message text`
	}
	if re.MatchString(err.Error()) { // want `\(\*regexp.Regexp\).MatchString depends on error message text`
	}
	if re.Match([]byte(err.Error())) { // want `\(\*regexp.Regexp\).Match depends on error message text`
	}
	if strings.Contains(fmt.Sprintf("%v", err), "x") { // want `strings.Contains depends on error message text`
	}
	if ok, _ := regexp.MatchString("x", fmt.Sprint(err)); ok { // want `regexp.MatchString depends on error message text`
	}
}

func good(err error, s string) {
	if errors.Is(err, io.EOF) {
	}
	if strings.Contains(s, "x") {
	}
	if s == "x" {
	}
	fmt.Println(err.Error()) // using the text for display is fine
	_ = fmt.Errorf("wrap: %w", err)
}

func ignored(err error) {
	if err.Error() == "legacy" { //hyrumcheck:ignore
	}
	//hyrumcheck:ignore
	if strings.Contains(err.Error(), "legacy") {
	}
}
