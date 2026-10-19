# ResetWatch engineering instructions

Follow Ponytail: the smallest complete solution, standard library first, no speculative abstractions, clear errors, and meaningful tests for nontrivial behavior. Do not remove validation, security, or accessibility to save lines.

## Cyclomatic complexity

Measure before refactoring. Project configuration wins; otherwise complexity 1–5 is fine, 6–10 should be watched, and 11+ must be refactored. Use gocyclo for Go. Decision points include conditions, loops, case branches, and boolean conditions.

Prefer guard clauses, well-named extracted functions, lookup tables, named predicates, and flat loops. Use strategies only for repeated type switches. Do not hide branching in dense expressions or game the metric. One responsibility per function.

Preserve behavior. Run tests before and after refactors. Do not break existing public APIs or exported signatures without asking. New code has no before score; label it new instead of inventing one.

For every refactor report a table of function, before CC, and after CC; name extracted helpers and state how behavior was verified. Measure all touched production functions, rank descending, and keep each at complexity 10 or lower.

## Product boundaries

Never send test emails to real recipients or run inference while checking quota. Use documented provider interfaces and local Codex or Claude Code sign-in; do not read or print auth tokens. One owner-approved exception: Claude usage comes from Claude Code's experimental `get_usage` control request (the data behind `/usage`, as used by the Claude Agent SDK), with no prompt sent. Keep it isolated in `internal/quota/claude*.go` and keep its risk documented. Keep simulated UI/demo data labeled. Fresh confirmed allowance and a previously scheduled reset are different claims.

No invented releases, repository URLs, adoption figures, or unsupported providers. Website installation instructions must match the built CLI. State production delivery limitations honestly.
