# Code Cleanliness

## Comments
- No comments in code (Exception: explain the why). Code must be self-documenting through clear naming.
- No godoc comments on types, functions, or methods. Names speak for themselves.
- No inline comments, no section separators (--- lines), no TODO markers.
- No commented-out code.
- Exception: required `//go:` directives and `//nolint:` directives.

## File size
- Source files must stay under 200 LOC (excluding tests).
- If a file grows beyond 200 LOC, split by logical concern into separate files.
- One responsibility per file. Name files after what they contain.

## Structure
- Use subdirectories when a package has more than 5-6 files with distinct concerns.
- Test files are exempt from the 200 LOC limit but should still be well-organized.
- Group related types/functions in the same file. Don't scatter a concept across files.

## Naming
- File names describe their content: `handler.go`, `writer.go`, `validate.go`.
- No generic names: `utils.go`, `helpers.go`, `common.go`, `misc.go`.
