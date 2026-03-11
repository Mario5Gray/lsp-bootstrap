package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

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
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	defer mgr.Shutdown(context.Background())

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

	httpServer := server.NewStreamableHTTPServer(s)

	log.Printf("lsp-mcp-bridge listening on :%s", cfg.Port)

	go func() {
		<-ctx.Done()
		log.Println("shutting down")
	}()

	if err := httpServer.Start(":" + cfg.Port); err != nil {
		log.Fatalf("server: %v", err)
	}
}
