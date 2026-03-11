# Issues. OnGoing

## First run



  ---
  1. Tool output formats are prose, not schemas

  hover → "type + doc string", call_hierarchy_in → "callers with locations", signature_help → "parameter names + types" — none define the actual shape returned to the agent. Is hover a plain string or a struct? Are locations absolute or relative paths?
  This needs concrete output schemas before implementation.

  2. -- in routing table args is wrong

  The table shows LSP_PYRIGHT_BIN + "-- --stdio" etc. — that -- separator was inherited from the mcp-language-server invocation pattern. In the bridge we call the LSP binary directly, so -- has no meaning and must be dropped. The args should just be
  ["--stdio"] or nothing.

  3. bridge.go vs main.go split is undefined

  The file structure lists both but assigns overlapping responsibilities (both do server setup). The build order only mentions main.go. It's unclear what lives in bridge.go that doesn't belong in main.go. Either collapse them or define the split
  explicitly.

  4. Restart policy is unspecified

  "Restarts dead clients" — how many times? Immediate or with backoff? What if it crashes on every initialize? Without a defined policy this becomes an infinite restart loop in pathological cases.

  5. "Clean shutdown" is undefined

  No timeout, no force-kill fallback. What if a language server ignores shutdown + exit? Needs: send LSP shutdown request, wait N seconds, send exit notification, SIGKILL after timeout.

  6. Multiple publishDiagnostics window is a magic number

  Edge case #3 says "500ms follow-up window" with no justification. This is a guess that will be wrong for slow indexers and wasteful for fast ones. Should either be configurable or replaced with a defined heuristic (e.g. wait until no notification
  arrives for 200ms, cap at 5s).

  7. gopls rootUri fallback is undefined

  Edge case #15 says "walk up to find nearest go.mod" but doesn't say what to do if there's no go.mod (non-module Go code, or a file outside the workspace). Needs an explicit fallback — probably LSP_WORKSPACE.

  8. .sc languageId is unspecified

  .sc (Scala script) is in the routing table but missing from the languageId values list. Metals expects scala for .sc — should be stated explicitly.

  9. Success criteria only exist for 3a and 3b

  Steps 1–4 have no validation gate. A broken lsp_client.go won't be caught until step 5. Each build order step needs a one-line "done when" condition.

  ---

