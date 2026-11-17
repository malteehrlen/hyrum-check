# hyrumcheck

A Go linter that fails when code depends on the text of an error message
(Hyrum's law: callers will come to rely on any observable behavior).

Flagged: `err.Error() == "..."`, `switch err.Error()`, `strings.Contains/HasPrefix/...`,
`regexp` matching on `err.Error()` or `fmt.Sprint(err)`, including via a local variable.
Use `errors.Is` / `errors.As` instead. Suppress with `//hyrumcheck:ignore`
on the same or preceding line.

    go install ./cmd/hyrumcheck
    hyrumcheck ./...          # exits non-zero on findings
    go vet -vettool=$(which hyrumcheck) ./...

## License

MIT, see [LICENSE](LICENSE).
