# codebook

Apply a codebook to documents with local models, and measure how far to trust the answers. Experimental: the API will change.

The name and the method come from qualitative research. A codebook defines each code in plain language, coders apply it to documents, agreement between coders is measured, and disagreements are reconciled. Here a local decision model is one coder and a person is the other.

## How it works

- **The codebook** is a YAML file of questions. Every label has a plain-language definition: decision models answer accurately only once each label is defined. A question can be marked `critical: true`.
- **Apply** sends every question about a document in one call to a decision model through the System One API (`POST /v1/systemone`, served by [Ollama](https://ollama.com)), and decodes yes/no (`noul`), `choice` and `score` answers into typed values. The client is a small standard-library one in `internal/systemone`, with request and response types copied from Ollama (MIT; notice kept).
- **Gates** route each answer by its confidence: accept it, ask a larger model, or ask a person. A critical yes/no question sends any answer with a probability of "true" at or above its flag threshold to a person. Answers that aren't accepted go to a disagreement log.
- **eval** applies the codebook to a spread sample of a folder's Markdown files and writes a CSV with an empty `correct` column. A person fills it with `y` or `n`.
- **agree** reads the labelled CSV and prints agreement by question, by folder and by confidence band, misses on critical questions, and proposed gate thresholds.

Requests go to the Ollama server it's given: `-endpoint`, else the codebook file's `endpoint`, else `OLLAMA_HOST` (read as Ollama's own client reads it), else `http://127.0.0.1:11434`. The codebook file's `model` names the decision model. Which servers and models are allowed is the server's business: configure Ollama for that. For an Ollama endpoint every result records the server version and the model's digest, and opening a model on a server version the tests weren't recorded against prints a warning; for other endpoints it records the endpoint and the model name each response reports. Every result records the codebook's hash. Output names files and reports labels, probabilities and counts, never document text.

## Codebook format

```yaml
name: garden-notes
model: sprout                                # the decision model
gate: {accept: 0.8, escalate: 0.5, flag: 0.2} # optional; these are the defaults
questions:
  - name: topic
    type: choice
    ask: Which topic is this note mainly about?
    labels:
      - {name: vegetables, definition: Growing food plants such as beans or squash.}
      - {name: soil, definition: Compost, mulch or anything about what plants grow in.}
  - name: private
    type: noul # yes/no: labels are exactly "false" and "true"
    ask: Does this note mention a neighbour by name?
    critical: true
    labels:
      - {name: "false", definition: No neighbour is named.}
      - {name: "true", definition: A neighbour is named, even in passing.}
  - name: finish
    type: score # levels from lowest to highest
    ask: How finished is this note?
    labels:
      - {name: scrap, definition: A fragment or a list of words.}
      - {name: draft, definition: Full sentences with a point, but rough.}
      - {name: ready, definition: A clear point, an example and a reason it matters.}
```

`Load` refuses a codebook whose question or definition is missing or starts with `TODO`.

Confidence is the server's measure of how strongly the model favours one label (1 − H(p)/ln N), not calibrated correctness. Start with the default gates, then use `agree` to set them from a labelled sample.

## Command

```sh
go install github.com/troyronda/codebook/cmd/codebook@latest

# Apply to files (or to paths on stdin, one per line); one JSON result per line.
codebook run -codebook codebook.yaml \
  [-escalate-model LARGER] [-log disagreements.jsonl] [-max-chars N] FILE...

# Sample 50 Markdown files across a folder's top-level folders and write a CSV to label.
codebook eval -codebook codebook.yaml \
  [-n 50] [-seed 1] [-max-chars 20000] [-skip attachments] -out eval.csv DIR

# Another endpoint.
codebook run -codebook codebook.yaml \
  -endpoint https://systemone.example -endpoint-kind other \
  -api-key-command "your-secret-tool get systemone-key" FILE...

# After filling the correct column with y or n:
codebook agree -codebook codebook.yaml [-group notes/,reading/] [-json] eval.csv
```

`eval` leaves out hidden folders, the folders named in `-skip`, iCloud conflict copies (`name 2.md` beside `name.md`) and files changed in the last 60 seconds. It sends at most `-max-chars` characters of each file, marks cut files in the `truncated` column, and writes the run's model, digest, server version and codebook hash to `eval.csv.meta.json`.

The endpoint can also be set in the codebook file:

```yaml
endpoint:
  url: https://systemone.example
  kind: other                                  # or ollama (the default)
  api_key_command: [your-secret-tool, get, systemone-key]
```

The API key is only ever read by running that command; it never appears in a file.

`run -escalate-model` asks the larger model only after the first model has finished every document, so one large model is loaded at a time. Unload the first with `ollama stop MODEL` between runs if memory is tight.

## Library

```go
cb, err := codebook.Load("codebook.yaml")      // or codebook.New(name, gate, questions)
m, err := codebook.Open(ctx, codebook.Endpoint{}, cb.ModelName) // zero Endpoint: OLLAMA_HOST, else local Ollama
res, err := cb.WithModel(m).Apply(ctx, codebook.Document{Path: path, Text: text})
```

`codebook.New` takes questions built in code, for callers that ask different questions at each step. `Document.State` sends a JSON value instead of text.

## Tests

The tests run against a fake server and against responses recorded from a local Ollama server (`testdata/recorded/<version>/`) for the made-up notes in `testdata/notes`. `TestLive` runs one note against a real server on `127.0.0.1:11434` when `CODEBOOK_LIVE_MODEL` names a model. To re-record: `go test -run TestRecorded -record -record-model MODEL`, then add the server version to `TestedVersions`.

Built with the help of AI coding tools. Maintained by Troy Ronda.
