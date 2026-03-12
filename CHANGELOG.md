# Changelog

All notable changes listed newest-first. Short entries only — see git log for full context.

---

## 2026-03-11

- `generate-env-lsp.sh` now derives a workspace-scoped port (7890–7989) and PID file from the workspace path so multiple workspaces can run independent bridge instances simultaneously.
- README refactored: updated architecture diagram, bridge quickstart, health endpoint docs, multi-workspace section, and removed stale Phase 3 "pending" references.
- `GET /mcp` hang documented in `issues.md` and `IMPL-3B.md`; smoke test corrected to use `/health`.
- Added `/health` endpoint to `lsp-mcp-bridge` returning uptime, version, and per-slot LSP status.
- Added `Manager.Health()` to expose slot state (configured, running, dead, failures, last_failure).
- `justfile` added to `lsp-mcp-bridge` with `build`, `install`, `test`, `test-verbose`, and `help` recipes.
- `build` outputs to `releases/`; `install` copies to `~/.local/bin` (overridable via `INSTALL_DIR`).
- Phase 3b complete: `rename`, `call_hierarchy_in`, `call_hierarchy_out`, `signature_help` tools implemented.
- `lsp-mcp-bridge` HTTP daemon wired into `start-lsp.sh` and `.mcp.json`.
- `TOP-LEVEL.md` written covering architecture, env contract, tool catalogue, test strategy, and ops.

## 2026-03-10

- Phase 3a complete: `hover`, `definition`, `references`, `diagnostics` tools implemented with full LSP client lifecycle.
- Lifecycle bug fixed in LSP client shutdown sequencing.
- Planning and acceptance test scaffolding committed (`ACCEPTANCE.md`, `IMPL-3A.md`, `IMPL-3B.md`, `MCP-BRIDGE.md`).
- `generate-java-lsp.sh` added: resolves `jdtls`, writes `jdtls-wrapper.sh`, injects `language-server-java` into `.mcp.json` and `~/.codex/config.toml`.
- Kotlin and Scala language server support added to `generate-env-lsp.sh` (`kotlin-language-server`, `metals`).
- Go language server support added to `generate-env-lsp.sh` (`gopls`).
- Shell, YAML, TOML, JSON, HTML, and CSS language server support added to `generate-env-lsp.sh`.
- `~/.codex/config.toml` is now updated automatically by `generate-env-lsp.sh` (no `--codex` flag required for Codex wiring).
- `--codex` flag added to wire the Codex CLI itself as an MCP server in `.mcp.json`.
- Initial commit: `generate-env-lsp.sh` with Python, JavaScript/TypeScript, and Rust support; `check-types.sh`, `start-lsp.sh`, `stop-lsp.sh` stubs; `env.lsp` generation; `.mcp.json` scaffolding.
