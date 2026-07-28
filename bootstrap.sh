# bootstrap.sh — reference only
#
# For actual bootstrapping, use bootstrap-repo.sh:
#   ./bootstrap-repo.sh /path/to/repo --all
#
# This file documents the manual sequence:
#
#   cd /path/to/repo
#   ../lsp-bootstrap/generate-env-lsp.sh --all
#   ../lsp-bootstrap/lsp-mcp-bridge/just install
#   make -f Makefile.lsp lsp-start
#   make -f Makefile.lsp lsp-health
#   make -f Makefile.lsp lsp-stop