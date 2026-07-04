# API Reference

The opencode-go-sdk exposes the OpenCode server's HTTP API through two client flavors and a small set of hand-written helpers.

## Clients

| Client | Constructor | Returns | Use for |
|---|---|---|---|
| `*opencode.Client` | `NewClient(server, opts...)` | `*http.Response` (raw) | SSE streaming, or when you decode bodies yourself |
| `*opencode.ClientWithResponses` | `NewClientWithResponses(server, opts...)` | `*<Op>Output` (typed) | Typed request/response cycles |

Both accept the same `ClientOption` variadics.

```go
client, _ := opencode.NewClient("http://localhost:4096")
typed, _ := opencode.NewClientWithResponses("http://localhost:4096")
```

## Client options

| Option | Source | Description |
|---|---|---|
| `opencode.WithHTTPClient(doer)` | generated | Override the underlying `*http.Client` |
| `opencode.WithBaseURL(url)` | generated | Override the server base URL |
| `opencode.WithRequestEditorFn(fn)` | generated | Per-request mutation hook |
| `opencode.WithBasicAuth(password)` | hand-written (`auth.go`) | HTTP Basic auth |
| `opencode.WithRequestEditor(fn)` | hand-written (`auth.go`) | Wrapper over `WithRequestEditorFn` |

## Method-name convention

oapi-codegen names client methods by PascalCasing the operation ID with dots removed. For example:

| Operation ID | `*Client` method |
|---|---|
| `session.create` | `SessionCreate` |
| `session.prompt_async` | `SessionPromptAsync` |
| `v2.session.list` | `V2SessionList` |
| `experimental.controlPlane.moveSession` | `ExperimentalControlPlaneMoveSession` |

On `*ClientWithResponses`, the method name gets a `WithResponse` suffix (e.g. `SessionCreateWithResponse`). The typed return is `*<Op>Output` (e.g. `*SessionCreateOutput`), whose `.JSON200` field holds the decoded response body.

Operations that take a request body also have a `WithBody` variant on `*Client` (e.g. `SessionCreateWithBody`) and a `WithBodyWithResponse` variant on `*ClientWithResponses`.

## v1 and v2 surfaces

The spec exposes two parallel API surfaces:

- **v1** (`/session/...`, `/permission/...`, `/question/...`) — the surface the examples use. Tags: `session`, `permission`, `question`.
- **v2** (`/api/session/...`, `/api/permission/...`, `/api/question/...`) — newer surface with additional endpoints (per-session event replay, compaction, context, history, wait, model/agent switching). Tags: `sessions`, `permissions`, `session questions`.

The SDK generates methods for both. Pick one surface and stay on it within a program.

## Hand-written helpers

A small layer of convenience code in `auth.go`, `prompt.go`, and the `sse/` package. These only reference exported generated types.

### `auth.go` — authentication

```go
// WithBasicAuth adds HTTP Basic authentication to every request made by the
// client.
func WithBasicAuth(password string) ClientOption

// WithRequestEditor adds a custom request editor to the client. A convenience
// wrapper around the generated WithRequestEditorFn.
func WithRequestEditor(fn RequestEditorFn) ClientOption
```

### `prompt.go` — prompt building

```go
// Ptr returns a pointer to v. Useful for optional fields in generated structs.
func Ptr[T any](v T) *T

// TextPromptBody builds a SessionPromptJSONBody with a single text part
// (synchronous — the server returns an AssistantMessage).
func TextPromptBody(text string) SessionPromptJSONBody

// TextPromptAsyncBody builds a SessionPromptAsyncJSONBody with a single text
// part for asynchronous prompts. Subscribe to the response via SSE.
func TextPromptAsyncBody(text string) SessionPromptAsyncJSONBody
```

### `sse` package — event streaming

```go
// ErrUnknownEventType is returned by EventAs/GlobalEventAs for unknown variants.
var ErrUnknownEventType = errors.New("opencode/sse: unknown event type")

// EventType extracts the "type" string from an Event. Returns "" if missing.
// Never panics.
func EventType(e opencode.Event) string

// EventAs deserializes an Event into a concrete variant type T.
func EventAs[T any](e opencode.Event) (*T, error)

// GlobalEventType extracts the "type" string from a GlobalEvent's payload.
func GlobalEventType(e opencode.GlobalEvent) string

// GlobalEventAs deserializes a GlobalEvent's payload into a concrete variant T.
func GlobalEventAs[T any](e opencode.GlobalEvent) (*T, error)

// SubscribeEvents opens an SSE stream on /event (per-instance events).
func SubscribeEvents(ctx context.Context, c *opencode.Client, params *opencode.EventSubscribeParams) (*Stream[opencode.Event], error

// SubscribeGlobalEvents opens an SSE stream on /global/event (system-wide).
func SubscribeGlobalEvents(ctx context.Context, c *opencode.Client) (*Stream[opencode.GlobalEvent], error)
```

### `sse.Stream[T]` — typed iterator

```go
type Stream[T any] struct { /* unexported fields */ }

func NewStream[T any](r io.Reader, closer io.Closer) *Stream[T]
func (s *Stream[T]) Next() bool      // reads + decodes next frame; false at EOF/error
func (s *Stream[T]) Event() T        // returns the most recent event
func (s *Stream[T]) Err() error     // error encountered during iteration
func (s *Stream[T]) Close() error    // closes the underlying response body
```

### SSE usage pattern

```go
stream, _ := sse.SubscribeEvents(ctx, client, nil)
defer stream.Close()

for stream.Next() {
    ev := stream.Event()
    switch sse.EventType(ev) {
    case "session.next.text.delta":
        d, _ := sse.EventAs[opencode.EventSessionNextTextDelta](ev)
        fmt.Print(d.Properties.Delta)
    case "session.idle":
        return
    }
}
if err := stream.Err(); err != nil { log.Fatal(err) }
```

Forward compatibility: adding an event variant upstream never breaks existing code — the new `type` string simply doesn't match any case in the switch.

## Common types

| Type | Description |
|---|---|
| `opencode.Session` | A chat session (id, title, model, agent, metadata, time) |
| `opencode.Event` | Union wrapper for per-instance SSE event variants |
| `opencode.GlobalEvent` | Union wrapper for system-wide SSE events (directory, payload, project) |
| `opencode.SessionCreateJSONRequestBody` | Body for `SessionCreate` (title, agent, model, parentID) |
| `opencode.SessionPromptJSONRequestBody` | Body for `SessionPrompt` (parts, agent, model) |
| `opencode.SessionPromptAsyncJSONRequestBody` | Body for `SessionPromptAsync` (same shape, non-blocking) |
| `opencode.VcsDiffParams` | Params for `VcsDiff` (directory, workspace, mode, context) |
| `opencode.VcsFileDiff` | One file's diff (file, patch, additions, deletions, status) |
| `opencode.PermissionRespondJSONRequestBody` | Body for `PermissionRespond` (response: always/once/reject) |
| `opencode.PermissionRespondJSONBodyResponseAlways` | Const: `"always"` |
| `opencode.PermissionRespondJSONBodyResponseOnce` | Const: `"once"` |
| `opencode.PermissionRespondJSONBodyResponseReject` | Const: `"reject"` |

Event variant types used in the examples: `EventSessionNextTextDelta`, `EventSessionNextTextEnded`, `EventSessionNextToolCalled`, `EventSessionNextToolSuccess`, `EventSessionNextToolFailed`, `EventSessionNextStepEnded`, `EventSessionIdle`, `EventSessionError`, `EventPermissionAsked`.

---

## Endpoint reference

All 188 operations across 162 paths, grouped by category. Near-duplicate tags are merged (e.g. `session` + `sessions`).

### Sessions

_Tags: `session`, `sessions`, `session questions` — the v1 and v2 session surfaces._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /session | session.list | List sessions | `SessionList` |
| POST | /session | session.create | Create session | `SessionCreate` |
| GET | /session/status | session.status | Get session status | `SessionStatus` |
| DELETE | /session/{sessionID} | session.delete | Delete session | `SessionDelete` |
| GET | /session/{sessionID} | session.get | Get session | `SessionGet` |
| PATCH | /session/{sessionID} | session.update | Update session | `SessionUpdate` |
| POST | /session/{sessionID}/abort | session.abort | Abort session | `SessionAbort` |
| GET | /session/{sessionID}/children | session.children | Get session children | `SessionChildren` |
| POST | /session/{sessionID}/command | session.command | Send command | `SessionCommand` |
| GET | /session/{sessionID}/diff | session.diff | Get message diff | `SessionDiff` |
| POST | /session/{sessionID}/fork | session.fork | Fork session | `SessionFork` |
| POST | /session/{sessionID}/init | session.init | Initialize session | `SessionInit` |
| GET | /session/{sessionID}/message | session.messages | Get session messages | `SessionMessages` |
| POST | /session/{sessionID}/message | session.prompt | Send message | `SessionPrompt` |
| DELETE | /session/{sessionID}/message/{messageID} | session.deleteMessage | Delete message | `SessionDeleteMessage` |
| GET | /session/{sessionID}/message/{messageID} | session.message | Get message | `SessionMessage` |
| DELETE | /session/{sessionID}/message/{messageID}/part/{partID} | part.delete | Delete a message part \* | `PartDelete` |
| PATCH | /session/{sessionID}/message/{messageID}/part/{partID} | part.update | Update a message part \* | `PartUpdate` |
| POST | /session/{sessionID}/permissions/{permissionID} | permission.respond | Respond to permission | `PermissionRespond` |
| POST | /session/{sessionID}/prompt_async | session.prompt_async | Send async message | `SessionPromptAsync` |
| POST | /session/{sessionID}/revert | session.revert | Revert message | `SessionRevert` |
| DELETE | /session/{sessionID}/share | session.unshare | Unshare session | `SessionUnshare` |
| POST | /session/{sessionID}/share | session.share | Share session | `SessionShare` |
| POST | /session/{sessionID}/shell | session.shell | Run shell command | `SessionShell` |
| POST | /session/{sessionID}/summarize | session.summarize | Summarize session | `SessionSummarize` |
| GET | /session/{sessionID}/todo | session.todo | Get session todos | `SessionTodo` |
| POST | /session/{sessionID}/unrevert | session.unrevert | Restore reverted messages | `SessionUnrevert` |
| GET | /api/session | v2.session.list | List sessions | `V2SessionList` |
| POST | /api/session | v2.session.create | Create session | `V2SessionCreate` |
| GET | /api/session/active | v2.session.active | List active sessions | `V2SessionActive` |
| GET | /api/session/{sessionID} | v2.session.get | Get session | `V2SessionGet` |
| POST | /api/session/{sessionID}/agent | v2.session.switchAgent | Switch session agent | `V2SessionSwitchAgent` |
| POST | /api/session/{sessionID}/compact | v2.session.compact | Compact session | `V2SessionCompact` |
| GET | /api/session/{sessionID}/context | v2.session.context | Get session context | `V2SessionContext` |
| GET | /api/session/{sessionID}/event | v2.session.events | Subscribe to session events | `V2SessionEvents` |
| GET | /api/session/{sessionID}/history | v2.session.history | Get session history | `V2SessionHistory` |
| POST | /api/session/{sessionID}/interrupt | v2.session.interrupt | Interrupt session execution | `V2SessionInterrupt` |
| GET | /api/session/{sessionID}/message/{messageID} | v2.session.message | Get session message | `V2SessionMessage` |
| POST | /api/session/{sessionID}/model | v2.session.switchModel | Switch session model | `V2SessionSwitchModel` |
| POST | /api/session/{sessionID}/prompt | v2.session.prompt | Send message | `V2SessionPrompt` |
| POST | /api/session/{sessionID}/revert/clear | v2.session.revert.clear | Clear staged revert | `V2SessionRevertClear` |
| POST | /api/session/{sessionID}/revert/commit | v2.session.revert.commit | Commit staged revert | `V2SessionRevertCommit` |
| POST | /api/session/{sessionID}/revert/stage | v2.session.revert.stage | Stage session revert | `V2SessionRevertStage` |
| POST | /api/session/{sessionID}/wait | v2.session.wait | Wait for session | `V2SessionWait` |
| GET | /api/session/{sessionID}/question | v2.session.question.list | List session question requests | `V2SessionQuestionList` |
| POST | /api/session/{sessionID}/question/{requestID}/reject | v2.session.question.reject | Reject pending question request | `V2SessionQuestionReject` |
| POST | /api/session/{sessionID}/question/{requestID}/reply | v2.session.question.reply | Reply to pending question request | `V2SessionQuestionReply` |
| GET | /api/question/request | v2.question.request.list | List pending question requests | `V2QuestionRequestList` |

### Events

_Tags: `event`, `events`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /event | event.subscribe | Subscribe to events | `EventSubscribe` |
| GET | /api/event | v2.event.subscribe | Subscribe to events | `V2EventSubscribe` |

### Permissions

_Tags: `permission`, `permissions`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /permission | permission.list | List pending permissions | `PermissionList` |
| POST | /permission/{requestID}/reply | permission.reply | Respond to permission request | `PermissionReply` |
| GET | /api/permission/request | v2.permission.request.list | List pending permission requests | `V2PermissionRequestList` |
| GET | /api/permission/saved | v2.permission.saved.list | List saved permissions | `V2PermissionSavedList` |
| DELETE | /api/permission/saved/{id} | v2.permission.saved.remove | Remove saved permission | `V2PermissionSavedRemove` |
| GET | /api/session/{sessionID}/permission | v2.session.permission.list | List session permission requests | `V2SessionPermissionList` |
| POST | /api/session/{sessionID}/permission | v2.session.permission.create | Create permission request | `V2SessionPermissionCreate` |
| GET | /api/session/{sessionID}/permission/{requestID} | v2.session.permission.get | Get permission request | `V2SessionPermissionGet` |
| POST | /api/session/{sessionID}/permission/{requestID}/reply | v2.session.permission.reply | Reply to pending permission request | `V2SessionPermissionReply` |

### Providers

_Tags: `provider`, `providers`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /provider | provider.list | List providers | `ProviderList` |
| GET | /provider/auth | provider.auth | Get provider auth methods | `ProviderAuth` |
| POST | /provider/{providerID}/oauth/authorize | provider.oauth.authorize | Start OAuth authorization | `ProviderOauthAuthorize` |
| POST | /provider/{providerID}/oauth/callback | provider.oauth.callback | Handle OAuth callback | `ProviderOauthCallback` |
| GET | /api/provider | v2.provider.list | List providers | `V2ProviderList` |
| GET | /api/provider/{providerID} | v2.provider.get | Get provider | `V2ProviderGet` |

### Instance

_Tag: `instance` — includes VCS endpoints._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /agent | app.agents | List agents | `AppAgents` |
| GET | /command | command.list | List commands | `CommandList` |
| GET | /formatter | formatter.status | Get formatter status | `FormatterStatus` |
| POST | /instance/dispose | instance.dispose | Dispose instance | `InstanceDispose` |
| GET | /lsp | lsp.status | Get LSP status | `LspStatus` |
| GET | /path | path.get | Get paths | `PathGet` |
| GET | /skill | app.skills | List skills | `AppSkills` |
| GET | /vcs | vcs.get | Get VCS info | `VcsGet` |
| POST | /vcs/apply | vcs.apply | Apply VCS patch | `VcsApply` |
| GET | /vcs/diff | vcs.diff | Get VCS diff | `VcsDiff` |
| GET | /vcs/diff/raw | vcs.diff.raw | Get raw VCS diff | `VcsDiffRaw` |
| GET | /vcs/status | vcs.status | Get VCS status | `VcsStatus` |

### File & search

_Tags: `file`, `filesystem`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /file | file.list | List files | `FileList` |
| GET | /file/content | file.read | Read file | `FileRead` |
| GET | /file/status | file.status | Get file status | `FileStatus` |
| GET | /find | find.text | Find text | `FindText` |
| GET | /find/file | find.files | Find files | `FindFiles` |
| GET | /find/symbol | find.symbols | Find symbols | `FindSymbols` |
| GET | /api/fs/find | v2.fs.find | Find files | `V2FsFind` |
| GET | /api/fs/list | v2.fs.list | List directory | `V2FsList` |
| GET | /api/fs/read/\* | v2.fs.read | Read file | `V2FsRead` |

### Configuration

_Tag: `config`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /config | config.get | Get configuration | `ConfigGet` |
| PATCH | /config | config.update | Update configuration | `ConfigUpdate` |
| GET | /config/providers | config.providers | List config providers | `ConfigProviders` |

### Global

_Tag: `global`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /global/config | global.config.get | Get global configuration | `GlobalConfigGet` |
| PATCH | /global/config | global.config.update | Update global configuration | `GlobalConfigUpdate` |
| POST | /global/dispose | global.dispose | Dispose instance | `GlobalDispose` |
| GET | /global/event | global.event | Get global events | `GlobalEvent` |
| GET | /global/health | global.health | Get health | `GlobalHealth` |
| POST | /global/upgrade | global.upgrade | Upgrade opencode | `GlobalUpgrade` |

### MCP

_Tag: `mcp`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /mcp | mcp.status | Get MCP status | `McpStatus` |
| POST | /mcp | mcp.add | Add MCP server | `McpAdd` |
| DELETE | /mcp/{name}/auth | mcp.auth.remove | Remove MCP OAuth | `McpAuthRemove` |
| POST | /mcp/{name}/auth | mcp.auth.start | Start MCP OAuth | `McpAuthStart` |
| POST | /mcp/{name}/auth/authenticate | mcp.auth.authenticate | Authenticate MCP OAuth | `McpAuthAuthenticate` |
| POST | /mcp/{name}/auth/callback | mcp.auth.callback | Complete MCP OAuth | `McpAuthCallback` |
| POST | /mcp/{name}/connect | mcp.connect | Connect to an MCP server \* | `McpConnect` |
| POST | /mcp/{name}/disconnect | mcp.disconnect | Disconnect from an MCP server \* | `McpDisconnect` |

### Integrations

_Tag: `integrations`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /api/integration | v2.integration.list | List integrations | `V2IntegrationList` |
| DELETE | /api/integration/attempt/{attemptID} | v2.integration.attempt.cancel | Cancel OAuth connection | `V2IntegrationAttemptCancel` |
| GET | /api/integration/attempt/{attemptID} | v2.integration.attempt.status | Get OAuth attempt status | `V2IntegrationAttemptStatus` |
| POST | /api/integration/attempt/{attemptID}/complete | v2.integration.attempt.complete | Complete OAuth connection | `V2IntegrationAttemptComplete` |
| GET | /api/integration/{integrationID} | v2.integration.get | Get integration | `V2IntegrationGet` |
| POST | /api/integration/{integrationID}/connect/key | v2.integration.connect.key | Connect with key | `V2IntegrationConnectKey` |
| POST | /api/integration/{integrationID}/connect/oauth | v2.integration.connect.oauth | Begin OAuth connection | `V2IntegrationConnectOauth` |

### Projects

_Tags: `project`, `projectCopy`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /project | project.list | List all projects | `ProjectList` |
| GET | /project/current | project.current | Get current project | `ProjectCurrent` |
| POST | /project/git/init | project.initGit | Initialize git repository | `ProjectInitGit` |
| PATCH | /project/{projectID} | project.update | Update project | `ProjectUpdate` |
| GET | /project/{projectID}/directories | project.directories | List project directories | `ProjectDirectories` |
| DELETE | /experimental/project/{projectID}/copy | v2.projectCopy.remove | Remove a project copy \* | `V2ProjectCopyRemove` |
| POST | /experimental/project/{projectID}/copy | v2.projectCopy.create | Create a project copy \* | `V2ProjectCopyCreate` |
| POST | /experimental/project/{projectID}/copy/generate-name | experimental.projectCopy.generateName | Generate project copy name | `ExperimentalProjectCopyGenerateName` |
| POST | /experimental/project/{projectID}/copy/refresh | v2.projectCopy.refresh | Refresh a project copy \* | `V2ProjectCopyRefresh` |

### PTY

_Tag: `pty` — v1 (`/pty/...`) and v2 (`/api/pty/...`) surfaces._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /pty | pty.list | List PTY sessions | `PtyList` |
| POST | /pty | pty.create | Create PTY session | `PtyCreate` |
| GET | /pty/shells | pty.shells | List available shells | `PtyShells` |
| DELETE | /pty/{ptyID} | pty.remove | Remove PTY session | `PtyRemove` |
| GET | /pty/{ptyID} | pty.get | Get PTY session | `PtyGet` |
| PUT | /pty/{ptyID} | pty.update | Update PTY session | `PtyUpdate` |
| GET | /pty/{ptyID}/connect | pty.connect | Connect to PTY session | `PtyConnect` |
| POST | /pty/{ptyID}/connect-token | pty.connectToken | Create PTY WebSocket token | `PtyConnectToken` |
| GET | /api/pty | v2.pty.list | List PTY sessions | `V2PtyList` |
| POST | /api/pty | v2.pty.create | Create PTY session | `V2PtyCreate` |
| DELETE | /api/pty/{ptyID} | v2.pty.remove | Remove PTY session | `V2PtyRemove` |
| GET | /api/pty/{ptyID} | v2.pty.get | Get PTY session | `V2PtyGet` |
| PUT | /api/pty/{ptyID} | v2.pty.update | Update PTY session | `V2PtyUpdate` |
| GET | /api/pty/{ptyID}/connect | v2.pty.connect | Connect to PTY session | `V2PtyConnect` |
| POST | /api/pty/{ptyID}/connect-token | v2.pty.connectToken | Create PTY WebSocket token | `V2PtyConnectToken` |

### Questions

_Tag: `question`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /question | question.list | List pending questions | `QuestionList` |
| POST | /question/{requestID}/reject | question.reject | Reject question request | `QuestionReject` |
| POST | /question/{requestID}/reply | question.reply | Reply to question request | `QuestionReply` |

### Sync

_Tag: `sync`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| POST | /sync/history | sync.history.list | List sync events | `SyncHistoryList` |
| POST | /sync/replay | sync.replay | Replay sync events | `SyncReplay` |
| POST | /sync/start | sync.start | Start workspace sync | `SyncStart` |
| POST | /sync/steal | sync.steal | Steal session into workspace | `SyncSteal` |

### TUI

_Tag: `tui`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| POST | /tui/append-prompt | tui.appendPrompt | Append TUI prompt | `TuiAppendPrompt` |
| POST | /tui/clear-prompt | tui.clearPrompt | Clear TUI prompt | `TuiClearPrompt` |
| GET | /tui/control/next | tui.control.next | Get next TUI request | `TuiControlNext` |
| POST | /tui/control/response | tui.control.response | Submit TUI response | `TuiControlResponse` |
| POST | /tui/execute-command | tui.executeCommand | Execute TUI command | `TuiExecuteCommand` |
| POST | /tui/open-help | tui.openHelp | Open help dialog | `TuiOpenHelp` |
| POST | /tui/open-models | tui.openModels | Open models dialog | `TuiOpenModels` |
| POST | /tui/open-sessions | tui.openSessions | Open sessions dialog | `TuiOpenSessions` |
| POST | /tui/open-themes | tui.openThemes | Open themes dialog | `TuiOpenThemes` |
| POST | /tui/publish | tui.publish | Publish TUI event | `TuiPublish` |
| POST | /tui/select-session | tui.selectSession | Select session | `TuiSelectSession` |
| POST | /tui/show-toast | tui.showToast | Show TUI toast | `TuiShowToast` |
| POST | /tui/submit-prompt | tui.submitPrompt | Submit TUI prompt | `TuiSubmitPrompt` |

### Workspaces

_Tag: `workspace`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /experimental/workspace | experimental.workspace.list | List workspaces | `ExperimentalWorkspaceList` |
| POST | /experimental/workspace | experimental.workspace.create | Create workspace | `ExperimentalWorkspaceCreate` |
| GET | /experimental/workspace/adapter | experimental.workspace.adapter.list | List workspace adapters | `ExperimentalWorkspaceAdapterList` |
| GET | /experimental/workspace/status | experimental.workspace.status | Workspace status | `ExperimentalWorkspaceStatus` |
| POST | /experimental/workspace/sync-list | experimental.workspace.syncList | Sync workspace list | `ExperimentalWorkspaceSyncList` |
| POST | /experimental/workspace/warp | experimental.workspace.warp | Warp session into workspace | `ExperimentalWorkspaceWarp` |
| DELETE | /experimental/workspace/{id} | experimental.workspace.remove | Remove workspace | `ExperimentalWorkspaceRemove` |

### Experimental

_Tag: `experimental` — capabilities, console, tools, worktrees, sessions._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /experimental/capabilities | experimental.capabilities.get | Get experimental capabilities | `ExperimentalCapabilitiesGet` |
| GET | /experimental/console | experimental.console.get | Get active Console provider metadata | `ExperimentalConsoleGet` |
| GET | /experimental/console/orgs | experimental.console.listOrgs | List switchable Console orgs | `ExperimentalConsoleListOrgs` |
| POST | /experimental/console/switch | experimental.console.switchOrg | Switch active Console org | `ExperimentalConsoleSwitchOrg` |
| GET | /experimental/resource | experimental.resource.list | Get MCP resources | `ExperimentalResourceList` |
| GET | /experimental/session | experimental.session.list | List sessions | `ExperimentalSessionList` |
| POST | /experimental/session/{sessionID}/background | experimental.session.background | Background subagents | `ExperimentalSessionBackground` |
| GET | /experimental/tool | tool.list | List tools | `ToolList` |
| GET | /experimental/tool/ids | tool.ids | List tool IDs | `ToolIds` |
| DELETE | /experimental/worktree | worktree.remove | Remove worktree | `WorktreeRemove` |
| GET | /experimental/worktree | worktree.list | List worktrees | `WorktreeList` |
| POST | /experimental/worktree | worktree.create | Create worktree | `WorktreeCreate` |
| POST | /experimental/worktree/reset | worktree.reset | Reset worktree | `WorktreeReset` |

### Control plane

_Tags: `control`, `controlPlane`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| DELETE | /auth/{providerID} | auth.remove | Remove auth credentials | `AuthRemove` |
| PUT | /auth/{providerID} | auth.set | Set auth credentials | `AuthSet` |
| POST | /log | app.log | Write log | `AppLog` |
| POST | /experimental/control-plane/move-session | experimental.controlPlane.moveSession | Move session | `ExperimentalControlPlaneMoveSession` |

### HTTP API (v2 misc)

_Tag: `opencode HttpApi` — v2 endpoints not covered by the categories above._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /api/agent | v2.agent.list | List agents | `V2AgentList` |
| DELETE | /api/credential/{credentialID} | v2.credential.remove | Remove credential | `V2CredentialRemove` |
| PATCH | /api/credential/{credentialID} | v2.credential.update | Update credential | `V2CredentialUpdate` |
| GET | /api/health | v2.health.get | Check server health | `V2HealthGet` |
| GET | /api/location | v2.location.get | Get location | `V2LocationGet` |

### Models

_Tag: `models`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /api/model | v2.model.list | List models | `V2ModelList` |

### Commands

_Tag: `commands`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /api/command | v2.command.list | List commands | `V2CommandList` |

### Skills

_Tag: `skills`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /api/skill | v2.skill.list | List skills | `V2SkillList` |

### Reference

_Tag: `reference`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /api/reference | v2.reference.list | List references | `V2ReferenceList` |

### Messages

_Tag: `messages`._

| Method | Path | Operation ID | Summary | Go method |
|---|---|---|---|---|
| GET | /api/session/{sessionID}/message | v2.session.messages | Get session messages | `V2SessionMessages` |

---

\* Summary inferred — the upstream spec does not provide a `summary` for this operation.
