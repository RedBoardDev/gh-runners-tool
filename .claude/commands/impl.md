---
description: Implement a feature from a spec. Reads the spec, plans, implements, tests.
---

# Implement from Spec

Implement $ARGUMENTS following this workflow:

1. **Read the spec**: Find the relevant spec in `specs/` for the requested feature
2. **Read architecture**: Check `specs/00-architecture.md` for package placement and interfaces
3. **Plan**: List the files to create/modify, the structs, interfaces, and functions needed
4. **Implement**: Write the code following the spec precisely
5. **Test**: Write tests alongside the implementation
6. **Verify**: Run `go build ./cmd/ghr` and `go test -race ./...`
7. **Review**: Check against the spec for any missed items

If the spec is ambiguous or contradicts another spec, flag it before implementing.
