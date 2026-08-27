# AGENTS.md

## Project Overview

This repository implements an AI agent microservice in Go using:

- **Gin** for the HTTP transport layer.
- **CloudWeGo Eino** for agent execution and orchestration.
- PostgreSQL may be added later for conversation persistence and vector retrieval.
- The service may eventually expose multiple externally visible agent roles.

The project is currently in an early implementation stage.

The immediate goal is to get **one agent working correctly end-to-end** before introducing abstractions for multiple agents, persistence, RAG, or multi-agent orchestration.

---

## Current Development Priority

Work in this order unless a task explicitly requires otherwise:

1. Implement one Eino-backed agent.
2. Verify direct invocation from Go without Gin.
3. Add at least one simple tool and verify the tool-call loop.
4. Introduce a small project-owned `Agent` abstraction if it is useful.
5. Expose the agent through Gin.
6. Add streaming support.
7. Add a second role.
8. Introduce an agent registry only when multiple roles actually exist.
9. Add conversation persistence.
10. Add retrieval / pgvector / RAG.
11. Add multi-agent orchestration only when a concrete use case requires it.

Do not build later-stage infrastructure prematurely.

---

## Architectural Principles

### 1. Gin is only the transport layer

Gin handlers must remain thin.

Handlers may:

- Parse path/query/body parameters.
- Perform HTTP-level validation.
- Read authentication context.
- Call application services.
- Convert application results/errors into HTTP responses.
- Stream application events to clients.

Handlers must not:

- Construct Eino agents.
- Contain prompts.
- Implement tool logic.
- Query databases directly.
- Implement conversation-history logic.
- Contain agent orchestration logic.

Preferred flow:

```text
HTTP request
    ↓
Gin handler
    ↓
application / agent service
    ↓
agent implementation
    ↓
Eino
    ↓
model / tools / retrieval
```

---

### 2. Do not couple the public API to Eino types

Eino is an implementation dependency.

Public HTTP DTOs and application-level DTOs should be owned by this project.

Avoid returning or accepting raw Eino structs through the HTTP API.

For example, prefer:

```go
type ChatRequest struct {
    Message string `json:"message"`
}
```

over exposing an Eino request type directly.

This allows Eino to be upgraded or replaced without forcing API changes.

---

### 3. Start with one concrete agent

Do not introduce an `AgentRegistry`, generic role factories, plugin systems, or complex dependency graphs until at least two concrete agents exist.

The initial structure may be as small as:

```text
cmd/
└── server/
    └── main.go

internal/
├── agent/
│   ├── agent.go
│   └── roles/
│       └── assistant.go
└── model/
    └── model.go
```

The first milestone is:

```text
main
 ↓
construct model
 ↓
construct Eino agent
 ↓
send a message
 ↓
receive response
```

This should work before Gin is added.

---

### 4. Prefer small project-owned interfaces

If an abstraction is needed, keep it minimal and driven by current requirements.

An acceptable initial abstraction is:

```go
type Agent interface {
    Run(ctx context.Context, message string) (string, error)
}
```

Do not add speculative methods such as:

```go
Pause(...)
Resume(...)
Save(...)
Restore(...)
Stream(...)
GetTools(...)
GetMemory(...)
GetMetadata(...)
```

unless actual code requires them.

Prefer evolving interfaces from real use cases instead of predicting future needs.

---

### 5. Agent construction happens at startup

Do not construct models, tools, graphs, or agents per HTTP request.

Long-lived dependencies should normally be initialized once during application startup and injected downward.

Preferred:

```text
main
 ├── config
 ├── model
 ├── tools
 ├── agent
 ├── services
 └── HTTP server
```

Avoid:

```go
func ChatHandler(c *gin.Context) {
    model := createModel()
    agent := createAgent(model)
    ...
}
```

unless a dependency is explicitly request-scoped.

---

## Agent Roles

The term "role" can mean different things. Keep them separate.

### API agent role

Examples:

- `assistant`
- `reviewer`
- `advisor`

These represent externally callable agent behaviors.

A future API may look like:

```http
POST /api/v1/agents/:role/chat
```

When multiple roles exist, they should normally share the same transport and service path.

Do not create independent copies of handler/service logic for every role.

---

### Authorization role

Examples:

- user
- admin
- operator

Authorization roles are security concepts and are not agent roles.

Never trust a client-provided agent or authorization role without validating permissions.

Tool access should be enforced in code, not only through prompts.

---

### Internal agent role

Examples:

- planner
- researcher
- evaluator

These may be implementation details of a larger Eino agent workflow.

Do not expose internal sub-agents as HTTP endpoints unless there is a real external API requirement.

---

## Eino Usage

### Prefer the simplest Eino abstraction that solves the problem

For a normal tool-using conversational agent:

```text
model
 ↓
reason / decide
 ↓
tool call
 ↓
tool result
 ↓
final response
```

prefer a standard Eino agent abstraction.

Do not introduce `compose.Graph` only because it exists.

Use graphs/workflows when the control flow itself is part of the application logic, for example:

```text
classify
 ↓
retrieve
 ↓
generate
 ↓
verify
 ├── fail → regenerate
 └── pass → return
```

---

### Keep prompts near agent definitions

Role-specific prompts should live with the role implementation.

Example:

```text
internal/
└── agent/
    └── roles/
        ├── assistant.go
        └── reviewer.go
```

Do not scatter system prompts through Gin handlers or unrelated infrastructure packages.

If prompts later become large or externally configurable, introduce a dedicated prompt package or resource directory at that time.

---

## Tools

Tools should be ordinary Go components with clear dependencies.

Prefer:

```text
agent
 ↓
Eino tool adapter
 ↓
application/service interface
 ↓
repository / external service
```

Avoid putting large amounts of business logic directly inside tool registration code.

---

### Tool permissions

Different agent roles may receive different tool sets.

Enforce this structurally.

Preferred:

```go
reviewerTools := []tool.BaseTool{
    searchDocs,
}

advisorTools := []tool.BaseTool{
    searchDocs,
    getUser,
}
```

Do not give every agent every tool and rely on a system prompt saying not to use certain tools.

---

## Streaming

Agent execution should eventually support streaming, but streaming does not need to exist in the first implementation.

When streaming is introduced, prefer an application-owned event model.

Example:

```go
type EventType string

const (
    EventMessageDelta EventType = "message_delta"
    EventToolStarted  EventType = "tool_started"
    EventToolFinished EventType = "tool_finished"
    EventDone         EventType = "done"
)

type Event struct {
    Type EventType `json:"type"`
    Data any       `json:"data"`
}
```

Translate Eino events into project-owned events before exposing them over SSE.

Do not expose Eino's internal event structs as the public protocol.

---

## Conversation Persistence

Conversation persistence is not part of the first milestone.

When it is added, persistence belongs outside the individual agent implementation.

Preferred flow:

```text
request
 ↓
application service
 ├── load conversation
 ↓
agent
 ↓
application service
 ├── persist input/output
 ↓
response
```

Agents should not issue SQL queries to reconstruct their own history.

---

## RAG / Vector Retrieval

Do not add RAG until ordinary agent execution and conversations work.

When retrieval is introduced:

- Prefer PostgreSQL + pgvector if it satisfies the project's actual requirements.
- Hide vector storage behind a retrieval interface.
- Keep embedding and vector-storage concerns outside Gin handlers.
- Do not couple agent prompts directly to SQL/vector-storage implementation details.

Possible future structure:

```text
internal/
├── retrieval/
│   ├── retriever.go
│   └── embedding.go
└── repository/
    └── postgres/
        └── vector.go
```

---

## Suggested Project Structure

Do not create every directory immediately. Add directories when corresponding functionality exists.

Expected long-term shape:

```text
.
├── cmd/
│   └── server/
│       └── main.go
│
├── internal/
│   ├── config/
│   │   └── config.go
│   │
│   ├── transport/
│   │   └── http/
│   │       ├── router.go
│   │       ├── handler/
│   │       ├── middleware/
│   │       └── dto/
│   │
│   ├── agent/
│   │   ├── service.go
│   │   ├── registry.go
│   │   ├── event.go
│   │   └── roles/
│   │
│   ├── model/
│   ├── tools/
│   ├── memory/
│   ├── retrieval/
│   └── repository/
│       └── postgres/
│
├── migrations/
├── Dockerfile
├── compose.yaml
├── go.mod
└── go.sum
```

`registry.go`, persistence packages, retrieval packages, and similar components should only be created once they are needed.

---

## Go Conventions

Follow idiomatic Go unless the repository establishes a more specific convention.

### Packages

- Keep packages focused and small.
- Avoid catch-all packages such as `utils`, `common`, or `helpers`.
- Prefer names describing responsibility.
- Avoid unnecessary package nesting.
- Keep implementation details under `internal/`.

### Interfaces

- Prefer small interfaces.
- Define interfaces near the consumer where practical.
- Do not create interfaces solely for "clean architecture".
- Concrete types are preferred when there is only one implementation and no testing or decoupling benefit.

### Errors

- Return errors; do not panic during normal request processing.
- Startup configuration/dependency failures may terminate startup.
- Add context when propagating errors when it materially improves diagnosis.
- Do not repeatedly wrap the same error with meaningless text.

### Context

- Pass `context.Context` as the first parameter.
- Propagate request contexts into Eino, database, and external-service calls.
- Do not store `context.Context` in long-lived structs.

### Concurrency

- Do not introduce goroutines without a clear lifecycle.
- Avoid detached/background goroutines from request handlers unless explicitly managed.
- Ensure shared state is concurrency-safe.
- Assume HTTP handlers may execute concurrently.

---

## HTTP Conventions

Tentative API shape:

```http
GET  /healthz

POST /api/v1/agents/:role/chat
POST /api/v1/agents/:role/chat/stream
```

The exact API may evolve.

Use:

- JSON for non-streaming API payloads.
- SSE for token/event streaming unless another transport is explicitly required.
- Appropriate HTTP status codes.
- Stable project-owned response/error structures.

Do not encode internal Go/Eino implementation details into URLs.

---

## Testing

Tests should focus first on project-owned behavior rather than Eino internals.

Prioritize:

1. Agent construction succeeds with valid dependencies.
2. Application service selects/invokes the expected agent.
3. Tool adapters call the expected service.
4. HTTP handlers correctly validate and translate requests/responses.
5. Repository logic works independently.
6. Streaming event translation is correct.

Avoid tests that merely duplicate Eino's own library tests.

For external LLM calls:

- Prefer interfaces/fakes where useful.
- Do not require real paid API calls for the normal unit-test suite.
- Integration tests using real models should be clearly separated.

Run before completing meaningful changes:

```bash
go test ./...
```

Also run:

```bash
go vet ./...
```

when practical.

Use the repository's formatter:

```bash
gofmt -w <changed-go-files>
```

---

## Dependency Policy

Before adding a dependency:

1. Check whether the standard library or an existing dependency already solves the problem.
2. Prefer mature, actively maintained libraries.
3. Keep framework-specific code localized.
4. Avoid adding large frameworks for small conveniences.

Eino-specific code should primarily live in the agent/model/tool integration layers.

Gin-specific code should primarily live in the HTTP transport layer.

---

## Configuration

Secrets must not be committed.

Examples include:

- LLM API keys.
- Database passwords.
- JWT secrets.
- External-service credentials.

Prefer environment-based configuration for deployment.

A future config type may contain:

```go
type Config struct {
    HTTP     HTTPConfig
    Database DatabaseConfig
    Model    ModelConfig
}
```

Do not access environment variables throughout unrelated packages. Load configuration centrally and inject typed configuration/dependencies.

---

## Database Policy

When PostgreSQL is introduced:

- Use migrations.
- Do not silently mutate schemas at application startup.
- Keep SQL/repository code out of Gin handlers and agent definitions.
- Prefer explicit SQL when it keeps behavior understandable.
- Keep transaction ownership at the application/repository layer where the business operation is defined.

Potential future entities include:

```text
conversation
message
```

Do not design their full schema until conversation requirements are known.

---

## Observability

Do not overbuild observability initially, but preserve enough boundaries to add it later.

Useful future dimensions include:

- request ID
- conversation ID
- agent role
- model
- model latency
- total agent latency
- tool name
- tool latency
- token usage
- errors

Never log secrets or complete sensitive payloads by default.

---

## Rules for Coding Agents

When modifying this repository:

1. Read the relevant existing code before proposing architectural changes.
2. Prefer the smallest change that satisfies the current requirement.
3. Do not create infrastructure for hypothetical future features.
4. Preserve separation between transport, agent logic, and infrastructure.
5. Do not move business logic into Gin handlers.
6. Do not expose Eino-specific structs as public API types.
7. Do not create an agent registry before multiple agents make it useful.
8. Do not add PostgreSQL, Redis, vector storage, RAG, or queues unless the current task requires them.
9. Do not convert simple agent behavior into a graph without a concrete control-flow requirement.
10. Do not add abstractions merely to match a design pattern.
11. Keep new interfaces minimal.
12. Run formatting and tests after code changes.
13. Explain any significant architectural deviation from this file.

---

## Current Immediate Task

Unless the repository has progressed beyond this point, prioritize implementing one working agent.

Target:

```text
program startup
 ↓
construct model
 ↓
construct one Eino agent
 ↓
send user message
 ↓
agent invokes model
 ↓
return/print response
```

After that works, add one simple tool and verify:

```text
message
 ↓
agent
 ↓
tool call
 ↓
tool result
 ↓
agent
 ↓
final response
```

Only then should HTTP integration become the next priority.
