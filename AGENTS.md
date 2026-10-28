# Rules for agents working in this repository

This repository is public from its first commit. Git history keeps everything, and Go's module proxy caches a tagged version permanently once anyone fetches it.

## Data

- Documents passed to this tool are processed only by local models through an Ollama server on the same machine. Never add a code path that sends document text to a hosted service.
- Never put document text in logs, errors, metrics or anything that leaves the machine. Output names files and reports labels, probabilities and counts.
- Only models on the caller's allowlist run. Any model name containing `cloud` is refused, even if it is listed.
- Record the Ollama server version and each model's digest on every run.
- Test fixtures are made-up documents only. Never commit real notes, eval output, labelled files, logs or anyone's configuration.
- Nothing here names a person's paths, Keychain items, schedules, models or projects. Those belong in the caller's own configuration.

## Git

- Work on branches and open pull requests; the maintainer merges.
- Ask before every push.
- No attribution anywhere: no `Co-Authored-By` trailers, no "Generated with" lines, no session links.
- Tags stay at `v0.x`, and a tag is made only after every check passes.
- Enable the hooks once per clone: `git config core.hooksPath .githooks`.

## Go

- One module at the repository root. Commands in `cmd/<name>/`; everything else in the root package or `internal/` until another module needs it. No `pkg/`.
- Package names are short, lowercase, singular nouns named for what they provide (no `util`, `common`, `tools` or plurals). Don't create a package before it has code.
- `go.work` is never committed. CI builds from `go.mod`.
- Adopt before building: prefer a maintained library to new code, and pin its version.
- Before a pull request: `gofmt`, `go vet`, `staticcheck`, `govulncheck`, `go test -race ./...` and `go mod tidy` with no diff.
