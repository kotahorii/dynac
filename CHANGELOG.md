# Changelog

All notable changes to this project will be documented in this file.

## [0.1.0] - 2026-01-25

### Added
- CLI with `generate` and `vet` commands.
- PartiQL subset parser with scan-safe validation rules.
- Code generation for `:one`, `:many`, and `:exec` queries (GetItem/Query/PutItem/Update/Delete).
- Runtime generation (`runtime_gen.go`) with error normalization and helper utilities.
- Batch write support with chunking and unprocessed retry handling.
- Index type hint binding enforcement for `S`/`N`/`B`.
- Sample app demonstrating CRUD usage.
- DynamoDB Local integration test using Testcontainers and a CI job to run it.
- Expanded README usage and guidance.

### Fixed
- `:exec` Update/Delete codegen now reuses the error variable correctly.
- Batch delete uses key-only marshaling to avoid non-key validation failures.
- Conditional imports avoid unused packages for batch-only codegen.
