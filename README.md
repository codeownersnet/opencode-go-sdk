# opencode-go-sdk

A Go client SDK for the [OpenCode](https://opencode.ai) server's HTTP API, generated from the official OpenAPI specification with hand-written helpers for SSE event streaming, authentication, and prompt building.

## Status

This SDK is **automatically regenerated** from the [upstream OpenAPI spec](https://raw.githubusercontent.com/anomalyco/opencode/refs/heads/dev/packages/sdk/openapi.json) on a weekly cadence via GitHub Actions. The committed [`opencode-spec.json`](opencode-spec.json) is a snapshot of the upstream spec; every regeneration produces a reviewable diff showing what changed upstream.

- **Full API coverage**: all 131 endpoints from the upstream spec.
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

## Architecture

The SDK has a clear separation between **generated** and **hand-written** code:

| Files | Description |
|---|---|
| `opencode.gen.go` | **Generated** by oapi-codegen from `opencode-spec.json`. Do not edit. |
| `opencode-spec.json` | Committed snapshot of the upstream OpenAPI spec. Diffs show upstream changes. |
| `auth.go` | Hand-written: `WithBasicAuth` client option. |
| `prompt.go` | Hand-written: `TextPromptBody`, `TextPromptAsyncBody`, `Ptr` helpers. |
| `sse/` | Hand-written: SSE frame reader, typed stream iterator, event dispatch helpers. |

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
