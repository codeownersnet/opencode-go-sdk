# opencode-go-sdk

A Go client SDK for the [OpenCode](https://opencode.ai) server's HTTP API, generated from the official OpenAPI specification with hand-written helpers for SSE event streaming, authentication, and prompt building.

## Status

This SDK is **automatically regenerated** from the [upstream OpenAPI spec](https://raw.githubusercontent.com/anomalyco/opencode/refs/heads/dev/packages/sdk/openapi.json) on a weekly cadence via GitHub Actions. The committed [`opencode-spec.json`](opencode-spec.json) is a snapshot of the upstream spec; every regeneration produces a reviewable diff showing what changed upstream.

- **Full API coverage**: 188 operations across 162 paths — see [API.md](API.md) for the full reference.
- **First-class SSE**: typed event streaming with discriminator-based dispatch (`EventType`, `EventAs[T]`).
- **Auto-versioned**: releases are cut automatically via [release-please](https://github.com/googleapis/release-please) when changes merge to `main`.

## Install

```bash
go get github.com/codeownersnet/opencode-go-sdk
```

## Usage

```go
package main

import (
    "context"
    "fmt"
    "log"

    opencode "github.com/codeownersnet/opencode-go-sdk"
    "github.com/codeownersnet/opencode-go-sdk/sse"
)

func main() {
    client, err := opencode.NewClient("http://localhost:4096")
    if err != nil {
        log.Fatal(err)
    }

    ctx := context.Background()

    // Subscribe to global events (SSE)
    stream, _ := sse.SubscribeGlobalEvents(ctx, client)
    defer stream.Close()

    // Create a session and send a prompt
    body := opencode.TextPromptBody("Explain this codebase")
    // resp, _ := client.SessionPromptWithBody(ctx, sessionID, body, nil)

    // Listen for events
    for stream.Next() {
        ev := stream.Event()
        switch sse.GlobalEventType(ev) {
        case "session.updated":
            su, _ := sse.GlobalEventAs[opencode.GlobalEventSessionUpdated](ev)
            fmt.Println("session updated:", su.Properties.SessionID)
        }
    }
}
```

## Examples

The [`examples/`](examples) directory contains runnable programs demonstrating different ways to use the SDK:

| Example | Description | Run |
|---|---|---|
| [`examples/quickstart/`](examples/quickstart/) | Quickstart: health check, SSE subscribe, session create, event loop. | `go run examples/quickstart/main.go` |
| [`examples/streaming-review/`](examples/streaming-review/) | Stream a code review in real time — print text deltas and log tool calls via SSE. | `go run examples/streaming-review/main.go` |
| [`examples/pr-review/`](examples/pr-review/) | Review a pull request with specialised AI agents (security, performance, code quality) plus a coordinator judge pass — multi-session orchestration on one SSE stream. | `go run examples/pr-review/main.go` |
| [`examples/parallel-scaffold/`](examples/parallel-scaffold/) | Generate multiple code artifacts concurrently using parallel sessions and synchronous prompts. | `go run examples/parallel-scaffold/main.go` |
| [`examples/auto-approve/`](examples/auto-approve/) | Auto-approve tool permissions for hands-off coding — monitor `permission.asked` events and respond automatically. | `go run examples/auto-approve/main.go` |
| [`examples/commit-message/`](examples/commit-message/) | Generate a Conventional Commit message from a git diff — composes VCS endpoints with session prompts. | `go run examples/commit-message/main.go` |
| [`examples/export-chat/`](examples/export-chat/) | Export a past conversation to a markdown file — lists sessions, retrieves messages, decodes `Message`/`Part` unions. | `go run examples/export-chat/main.go` |

All examples require a running opencode server (`opencode serve &`) and default to `http://localhost:4096`. Set `OPENCODE_SERVER` to target a different host.

## API reference

Every endpoint and hand-written helper is documented in [API.md](API.md).

## Architecture

The SDK has a clear separation between **generated** and **hand-written** code:

| Files | Description |
|---|---|
| `opencode.gen.go` | **Generated** by oapi-codegen from `opencode-spec.json`. Do not edit. |
| `opencode-spec.json` | Committed snapshot of the upstream OpenAPI spec. Diffs show upstream changes. |
| `auth.go` | Hand-written: `WithBasicAuth` client option. |
| `prompt.go` | Hand-written: `TextPromptBody`, `TextPromptAsyncBody`, `Ptr` helpers. |
| `sse/` | Hand-written: SSE frame reader, typed stream iterator, event dispatch helpers. |
| `API.md` | Hand-maintained reference for every endpoint and helper. |

The generated code is never hand-edited. The regen CI guardrail asserts that `make generate` only touches `*.gen.go` files. See [AGENTS.md](AGENTS.md) for the full development standards.

## Regeneration

```bash
make pull       # fetch upstream spec
make generate   # normalize + run oapi-codegen
make test       # build + test
make regen      # pull + generate + test (the full refresh loop)
```

## Disclaimer

**This project is not affiliated with, endorsed by, or part of the OpenCode / [anomaly](https://anoma.ly) development team.** It is an independent, community-maintained Go client for the OpenCode server's public HTTP API.

We gratefully support the OpenCode project's work and efforts. For the official OpenCode documentation, visit [opencode.ai/docs](https://opencode.ai/docs/). For the official (Stainless-generated) Go SDK, see [`github.com/sst/opencode-sdk-go`](https://github.com/anomalyco/opencode-sdk-go).

## License

MIT
