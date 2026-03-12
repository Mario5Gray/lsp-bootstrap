 ---
  Phase 2 (prerequisite, lightweight)
  - Document host-local deployment decision — add a note to README.md or env.custom covering the rationale (filesystem access, index persistence, no Docker yet)

  Phase 3a — Bridge MVP
  - Confirm mcp-language-server exposes the 4 required tools (hover, definition, references, diagnostics) — or decide to build custom
  - If custom: scaffold lsp-mcp-bridge/ — config.py, lsp_client.py, manager.py, bridge.py per §3.4
  - Implement lsp/hover (textDocument/hover)
  - Implement lsp/definition (textDocument/definition)
  - Implement lsp/references (textDocument/references)
  - Implement lsp/diagnostics — pull-based: didOpen → wait for publishDiagnostics → return
  - Wire into Claude Code via HTTP transport (localhost:7890/mcp) or confirm stdio wiring is sufficient
  - Validate: hover on worker_pool.py fut field returns Future[tuple[bytes, int]]

  Phase 3b — Extended Tools
  - Implement lsp/rename — returns dry-run diff per file, no apply
  - Implement lsp/call_hierarchy_in (callHierarchy/incomingCalls)
  - Implement lsp/call_hierarchy_out (callHierarchy/outgoingCalls)
  - Implement lsp/signature_help (textDocument/signatureHelp)
  - Update CLAUDE.md tool selection table (treesitter vs LSP decision guide)

  ---


  ┌───────────────┬─────────────────────────────┬───────────────────────────────────────────────┬──────────────────────────────────────┐
  │   Language    │           Binary            │                    Install                    │                Notes                 │
  ├───────────────┼─────────────────────────────┼───────────────────────────────────────────────┼──────────────────────────────────────┤
  │ JSON          │ vscode-json-language-server │ npm i -g vscode-langservers-extracted         │ Schema validation, hover docs        │
  ├───────────────┼─────────────────────────────┼───────────────────────────────────────────────┼──────────────────────────────────────┤
  │ TOML          │ taplo                       │ cargo install taplo-cli                       │ Great for Cargo.toml, pyproject.toml │
  ├───────────────┼─────────────────────────────┼───────────────────────────────────────────────┼──────────────────────────────────────┤
  │ HTML          │ vscode-html-language-server │ same vscode-langservers-extracted             │                                      │
  ├───────────────┼─────────────────────────────┼───────────────────────────────────────────────┼──────────────────────────────────────┤
  │ CSS/SCSS      │ vscode-css-language-server  │ same vscode-langservers-extracted             │                                      │
  ├───────────────┼─────────────────────────────┼───────────────────────────────────────────────┼──────────────────────────────────────┤
  │ Dockerfile    │ docker-langserver           │ npm i -g dockerfile-language-server-nodejs    │                                      │
  ├───────────────┼─────────────────────────────┼───────────────────────────────────────────────┼──────────────────────────────────────┤
  │ Terraform/HCL │ terraform-ls                │ brew install hashicorp/tap/terraform-ls       │ HashiCorp official                   │
  ├───────────────┼─────────────────────────────┼───────────────────────────────────────────────┼──────────────────────────────────────┤
  │ GraphQL       │ graphql-lsp                 │ npm i -g graphql-language-service-cli         │                                      │
  ├───────────────┼─────────────────────────────┼───────────────────────────────────────────────┼──────────────────────────────────────┤
  │ SQL           │ sqls                        │ go install github.com/sqls-server/sqls@latest │                                      │
  ├───────────────┼─────────────────────────────┼───────────────────────────────────────────────┼──────────────────────────────────────┤
  │ Lua           │ lua-language-server         │ brew install lua-language-server              │                                      │
  ├───────────────┼─────────────────────────────┼───────────────────────────────────────────────┼──────────────────────────────────────┤
  │ C/C++         │ clangd                      │ brew install llvm / apt install clangd        │                                      │
  └───────────────┴─────────────────────────────┴───────────────────────────────────────────────┴──────────────────────────────────────┘

