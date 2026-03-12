package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	var (
		envLsp    = flag.String("env-lsp", "env.lsp", "Path to env.lsp config file")
		envCustom = flag.String("env-custom", "", "Optional path to env.custom override file")
		workspace = flag.String("workspace", "", "Override LSP_WORKSPACE")
		port      = flag.String("port", "", "Override LSP_PORT")
	)
	flag.Parse()

	flags := map[string]string{}
	if *workspace != "" {
		flags["LSP_WORKSPACE"] = *workspace
	}
	if *port != "" {
		flags["LSP_PORT"] = *port
	}

	cfg, err := Load(*envLsp, *envCustom, flags)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if cfg.Workspace == "" {
		cwd, _ := os.Getwd()
		cfg.Workspace = cwd
	}
	if cfg.Port == "" {
		cfg.Port = "7890"
	}

	mgr := NewManager(cfg)

	s := server.NewMCPServer("lsp-mcp-bridge", "0.1.0")

	s.AddTool(mcp.NewTool("hover",
		mcp.WithDescription("Get type and documentation for a symbol at a position"),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Absolute path to the source file")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("Line number (1-based)")),
		mcp.WithNumber("column", mcp.Required(), mcp.Description("Column number (1-based)")),
	), hoverHandler(mgr))

	s.AddTool(mcp.NewTool("definition",
		mcp.WithDescription("Find the definition location of a symbol"),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Absolute path to the source file")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("Line number (1-based)")),
		mcp.WithNumber("column", mcp.Required(), mcp.Description("Column number (1-based)")),
	), definitionHandler(mgr))

	s.AddTool(mcp.NewTool("references",
		mcp.WithDescription("Find all references to the symbol at a position"),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Absolute path to the source file")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("Line number (1-based)")),
		mcp.WithNumber("column", mcp.Required(), mcp.Description("Column number (1-based)")),
	), referencesHandler(mgr))

	s.AddTool(mcp.NewTool("diagnostics",
		mcp.WithDescription("Get diagnostics (errors, warnings) for a file"),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Absolute path to the source file")),
	), diagnosticsHandler(mgr))

	s.AddTool(mcp.NewTool("rename",
		mcp.WithDescription("Rename a symbol and return a unified diff (nothing applied to disk)"),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Absolute path to the source file")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("Line number (1-based)")),
		mcp.WithNumber("column", mcp.Required(), mcp.Description("Column number (1-based)")),
		mcp.WithString("newName", mcp.Required(), mcp.Description("New symbol name")),
	), renameHandler(mgr))

	s.AddTool(mcp.NewTool("call_hierarchy_in",
		mcp.WithDescription("Return all callers of the symbol at a position"),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Absolute path to the source file")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("Line number (1-based)")),
		mcp.WithNumber("column", mcp.Required(), mcp.Description("Column number (1-based)")),
	), callHierarchyInHandler(mgr))

	s.AddTool(mcp.NewTool("call_hierarchy_out",
		mcp.WithDescription("Return all callees of the symbol at a position"),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Absolute path to the source file")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("Line number (1-based)")),
		mcp.WithNumber("column", mcp.Required(), mcp.Description("Column number (1-based)")),
	), callHierarchyOutHandler(mgr))

	s.AddTool(mcp.NewTool("signature_help",
		mcp.WithDescription("Return parameter names and types for the call at a position"),
		mcp.WithString("filePath", mcp.Required(), mcp.Description("Absolute path to the source file")),
		mcp.WithNumber("line", mcp.Required(), mcp.Description("Line number (1-based)")),
		mcp.WithNumber("column", mcp.Required(), mcp.Description("Column number (1-based)")),
	), signatureHelpHandler(mgr))

	startTime := time.Now()

	mux := http.NewServeMux()

	// /health — quick liveness + slot stats (GET)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		type response struct {
			Status  string                `json:"status"`
			Uptime  string                `json:"uptime"`
			Version string                `json:"version"`
			Slots   map[string]SlotStatus `json:"slots"`
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response{ //nolint:errcheck
			Status:  "ok",
			Uptime:  time.Since(startTime).Round(time.Second).String(),
			Version: "0.1.0",
			Slots:   mgr.Health(),
		})
	})

	httpServer := server.NewStreamableHTTPServer(s,
		server.WithStreamableHTTPServer(&http.Server{Handler: mux}),
	)
	// Mount the MCP handler into our mux at /mcp
	mux.Handle("/mcp", httpServer)

	log.Printf("lsp-mcp-bridge listening on :%s", cfg.Port)

	go func() {
		if err := httpServer.Start(":" + cfg.Port); err != nil {
			log.Printf("server stopped: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	log.Println("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	httpServer.Shutdown(shutCtx) //nolint:errcheck
	mgr.Shutdown(shutCtx)
}
