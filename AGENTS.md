# Agents working in this repo

A library and command that apply a codebook to documents with decision models and measure how far to trust the answers. Public.

## Build and test

```sh
go build ./...
go test -race ./...
CODEBOOK_LIVE_MODEL=<model> go test -run TestLive .   # against a local Ollama server
```

CI (`.github/workflows/ci.yml`) runs the shared `go` and `private-terms` workflows from `troyronda/.github` on every pull request: gofmt, a `go mod tidy` diff, `go vet`, staticcheck, govulncheck, the race tests, gitleaks and the private-terms check.

## Layout

- One module at the repo root. The `codebook` package is the root; the command is in `cmd/codebook/`; everything else is in `internal/` (`systemone` is the System One client, `sample` picks eval files, `fakeollama` and `testvault` serve tests). No `pkg/`.
- `asker.go` is the only file that knows which System One client is used.
- Package names are short, lowercase, singular nouns. Don't create a package before it has code.
- `go.work` is never committed; CI builds from `go.mod`.

## Rules

- Test fixtures are made-up notes only. Recorded responses in `testdata/recorded/` come from asking a local server about those notes, never real ones.
- No secrets in the repo, in tests or in logs. Output, errors and logs never include document text. API keys are read only by running a configured command.
- Dependencies: the standard library, official SDKs, or widely used and maintained libraries, pinned in `go.mod`. No dependency on small single-maintainer repositories.
- Tags stay at `v0.x`, are made only after every check passes, and never move.
