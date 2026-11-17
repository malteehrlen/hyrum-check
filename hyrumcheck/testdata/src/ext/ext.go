package ext

import "errors"

var ErrNotFound = errors.New("not found")

type Error struct{ Code int }

func (e *Error) Error() string { return "ext error" }

func Get() (int, error)    { return 0, ErrNotFound }
func Legacy() (int, error) { return 0, ErrNotFound }
