# .claude/ — Project Configuration

## Commands

| Command | Description |
|---|---|
| `/pm` | Project manager mode — discuss, plan, delegate |
| `/impl <feature>` | Implement a feature from specs |
| `/test [packages]` | Run tests with race detector |
| `/check` | Full CI pipeline (build, vet, fmt, lint, test) |

## Agents

| Agent | Role |
|---|---|
| `spec-writer` | Write/update specs from a brief |
| `developer` | Implement Go code from specs |
| `code-reviewer` | Review code quality and idioms |
| `spec-checker` | Verify implementation matches specs |

## Structure

```
.claude/
├── CLAUDE.md              # Project instructions (always loaded)
├── settings.json          # Permissions, env vars
├── rules/                 # Auto-loaded by file type
│   ├── go-style.md        #   *.go → naming, errors, concurrency
│   ├── architecture.md    #   internal/** → packages, interfaces, DI
│   ├── testing.md         #   *_test.go → table-driven, mocking
│   └── security.md        #   *.go, *.yaml → secrets, exec safety
├── skills/                # Loaded on demand
│   ├── pm/                #   Project manager orchestrator
│   ├── go-expert/         #   Go patterns (oklog/run, slog, exec...)
│   └── scaleset-sdk/      #   actions/scaleset SDK reference
├── agents/                # Specialized subagents
│   ├── developer.md
│   ├── spec-writer.md
│   ├── code-reviewer.md
│   └── spec-checker.md
└── commands/              # Slash commands
    ├── pm.md
    ├── impl.md
    ├── test.md
    └── check.md
```
