# hyrumcheck

A Go linter that fails when code depends on the text of an error message.

Hyrum's law says that any observable behavior of an API will eventually be
relied upon. Error strings are observable, so callers end up matching on them
(`strings.Contains(err.Error(), "not found")`), and a harmless rewording in a
dependency then breaks them silently. `hyrumcheck` flags that pattern.

## Install

Download a binary from the [releases page](https://github.com/malteehrlen/hyrum-check/releases),
or with Go:

    go install github.com/malteehrlen/hyrum-check/cmd/hyrumcheck@latest

## Usage

    hyrumcheck ./...                                  # exits non-zero on findings
    go vet -vettool=$(which hyrumcheck) ./...         # as a vet tool

Example:

    main.go:14:5: strings.Contains depends on error message text; error originates in os; try errors.Is with os.ErrExist, ... or errors.As with *os.PathError

### What is flagged

- `err.Error() == "..."` (and `!=`, `<`, ...), and `switch err.Error()`
- `strings` / `bytes` matching functions on error text (`Contains`, `HasPrefix`, `Cut`, ...)
- `regexp` matching on error text
- `fmt.Sprint(err)` / `fmt.Sprintf("%v", err)` used the same way, and error
  text held in a local variable

Printing, logging and wrapping (`%w`) errors is fine.

### Where the error came from matters

The linter traces the origin of the error (`x, err := pkg.F()`):

- Errors from other modules, including the standard library, are reported,
  and the message suggests sentinel or typed errors the origin package exports.
- Errors from your own module are skipped unless you pass `-internal`.
- `_test.go` files are skipped unless you pass `-tests`.

### Options

| Flag | Meaning |
| --- | --- |
| `-internal` | also report errors that originate in your own module |
| `-tests` | also check `_test.go` files |
| `-allow list` | comma-separated functions whose errors may be matched on text, e.g. `github.com/lib/pq.*,ext.Legacy`; a trailing `*` matches any suffix |

To suppress a single finding, add `//hyrumcheck:ignore` on the same line or the
line above.

### Limitations

Tracking is straight-line and by source order; it does not follow error text
through function arguments or struct fields. Errors whose origin can't be
determined are treated as external.

## Releasing

Push a version tag; the workflow runs vet and tests, builds binaries for
linux, macOS and windows (amd64/arm64 where applicable), and publishes a
GitHub release with checksums.

    git tag v0.1.0 && git push origin v0.1.0

## License

MIT, see [LICENSE](LICENSE).
