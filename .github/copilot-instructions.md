# GitHub Copilot Instructions for Pebble

This file provides guidance for GitHub Copilot when working on the Pebble project.

## Project Overview

Pebble is a lightweight Linux service manager written in Go that helps orchestrate local processes. It features layered configuration, service dependencies, health checks, and an HTTP API.

## Development Setup

### Building and Running

- Use `go run ./cmd/pebble` to run during development (automatically recompiles)
- Use `go install ./cmd/pebble` to install to `~/go/bin`
- Set `$PEBBLE` environment variable to the working directory (e.g., `export PEBBLE=~/pebble`)
- Run the daemon: `go run ./cmd/pebble run`

### Testing

- Run all tests: `go test ./...`
- Run tests with race detector: `go test -race ./...`
- Run integration tests: `go test -count=1 -tags=integration ./tests/`
- Run specific test: `go test ./cmd/pebble -v -check.v -check.f TestName`

### Linting

- Configuration is in `.github/.golangci.yml`
- Run linter using GitHub Actions workflow or directly with golangci-lint

## Code Style Guidelines

### Key Principles

1. **Follow STYLE.md**: Read and follow the comprehensive style guide in [STYLE.md](../STYLE.md)
2. **Follow HACKING.md**: See [HACKING.md](../HACKING.md) for development practices

### Naming Conventions

- Use `MixedCaps` for exported names, `mixedCaps` for local/non-exported
- Abbreviations should have consistent case: `HTTPPort` or `httpPort`, not `HttpPort`
- Use `MyConst` for constants, not `ALL_CAPS` (except environment variables)
- Keep names concise and contextually appropriate
- Be consistent with existing naming patterns

### Imports Organization

Organize imports in three groups (alphabetically sorted within each):
1. Standard library
2. Third-party imports
3. Pebble imports (`github.com/canonical/pebble/...`)

Test files should import `gopkg.in/check.v1` as `. "gopkg.in/check.v1"`

### Error Handling

- Start error messages with "cannot" (not "failed to" or "error")
- Error messages should be lowercase: `fmt.Errorf("cannot open file: %w", err)`
- Be specific and user-focused in error messages
- Use `errors.Is()` instead of `==` for error checking
- Wrap errors with context using `%w`: `fmt.Errorf("cannot get logs: %w", err)`
- Use custom error types for recoverable errors

### Log Messages

- Log messages should start with a capital letter
- Use "Cannot X" not "Error Xing": `logger.Noticef("Cannot marshal logs: %v", err)`

### Code Formatting

- Merge arguments of same type: `func foo(a, b, c string)`
- Use `:=` for short variable declarations when possible
- Use "cuddled braces" for multi-line struct literals
- Add trailing commas in multi-line structs/slices
- Avoid very small non-exported helper functions
- Group related struct fields together
- For octal literals, prefer `0o755` over `0755`

### Testing Conventions

- Use meaningful test names following existing patterns
- Use `gopkg.in/check.v1` test framework
- Use metasyntactic variables (`foo`, `bar`) or follow existing test conventions
- Use `t.Setenv()` for environment variables in tests
- Use `t.Fatalf()` when tests should not continue
- Check behavior, not log output
- Add comments for complex tests
- Avoid obvious comments

### Commit Messages

Follow conventional commit style:
```
feat: description
fix: description
test: description
docs: description
chore: description
```

Use brackets for component scope: `feat(daemon): description`

## Common Patterns

### String Operations

- Simple concatenation: use `+` not `fmt.Sprintf()`
- Complex formatting: use `fmt.Sprintf()`
- Multiline strings in tests: use `[1:]` for readability

### Concurrency

- Use unbuffered channels for cancellation (close to cancel)
- Prefer `time.After` over `time.Sleep` for cancelable waits
- Avoid locking in helper functions; manage locks at higher level

### Regular Expressions

- Compile regex at package level using `regexp.MustCompile()`
- Don't compile in functions (expensive operation)

## Project Structure

- `cmd/pebble` - CLI command implementation
- `client/` - Go client for Pebble API
- `internals/` - Core implementation packages
- `docs/` - Documentation (Sphinx-based)
- `tests/` - Integration tests

## Documentation

- See [HACKING.md](../HACKING.md) for development workflow
- See [STYLE.md](../STYLE.md) for complete style guide
- See [README.md](../README.md) for project overview
- Online docs: https://documentation.ubuntu.com/pebble/

## Additional Notes

- Go version requirement: see `go.mod` (currently 1.24.6)
- Main test framework: `gopkg.in/check.v1`
- API documentation is in OpenAPI spec: `docs/specs/openapi.yaml`
- CLI reference docs auto-generated: `make cli-help` in `docs/` directory
