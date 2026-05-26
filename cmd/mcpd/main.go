// Command mcpd exposes the auto-bot kanban over the Model Context Protocol
// so any LLM client (Claude Code, Cursor, Claude Agent SDK scripts) can
// read and mutate the board via the same audit + risk-classification path
// the voice tools use.
//
// Sprint 2.0 status: foundational slice. Five tools, two transports
// (stdio + HTTP), single-token HTTP auth, in-memory board adapter. The
// shared-state-with-cmd/server story is the open architectural question
// for S2.1; see internal/mcp/tools.go's InMemoryBoardAdapter for the
// rationale.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/somoore/auto-bot/internal/agent"
	"github.com/somoore/auto-bot/internal/board"
	"github.com/somoore/auto-bot/internal/mcp"
	"github.com/somoore/auto-bot/internal/mocks"
)

func main() {
	var (
		transport = flag.String("transport", envOr("MCPD_TRANSPORT", "stdio"), "transport: stdio or http")
		port      = flag.Int("port", envOrInt("MCPD_PORT", 4000), "HTTP listen port (when --transport=http)")
		boardID   = flag.String("board-id", envOr("APP_BOARD_ID", "default"), "board identifier")
		tenantID  = flag.String("tenant-id", envOr("APP_TENANT_ID", "default"), "tenant identifier")
	)
	flag.Parse()

	token := os.Getenv("MCPD_TOKEN")
	if *transport == "http" && token == "" {
		log.Println("mcpd: WARNING — MCPD_TOKEN is empty; HTTP transport will accept anonymous requests")
	}

	sqlitePath := os.Getenv("BOARD_SQLITE_PATH")
	if sqlitePath != "" {
		// #nosec G706 -- sqlitePath is run through sanitizeForLog before interpolation; gosec's taint analysis does not recognize the sanitizer wrapper.
		log.Printf("mcpd: BOARD_SQLITE_PATH=%q noted; running with in-memory adapter (shared SQLite adapter lands in S2.1)", sanitizeForLog(sqlitePath))
	}

	adapter := mcp.NewInMemoryBoardAdapter()
	seedDefaultCards(adapter, *tenantID, *boardID)

	runStore := mocks.NewRunStore()
	coordinator := agent.NewSimpleRunCoordinator(runStore, nil)

	tools := mcp.BuildTools(mcp.ToolDeps{
		Board:        adapter,
		RunStore:     runStore,
		Coordinator:  coordinator,
		TenantID:     *tenantID,
		BoardID:      *boardID,
		DefaultActor: "mcp",
	})
	server := mcp.NewServer(tools)
	server.AuthToken = token

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	switch strings.ToLower(*transport) {
	case "stdio":
		log.Printf("mcpd: serving stdio transport (tenant=%s board=%s)", sanitizeForLog(*tenantID), sanitizeForLog(*boardID))
		if err := server.ServeStdio(ctx, os.Stdin, os.Stdout); err != nil && !errors.Is(err, context.Canceled) {
			log.Fatalf("mcpd: stdio: %v", err)
		}
	case "http":
		addr := fmt.Sprintf(":%d", *port)
		mux := http.NewServeMux()
		mux.Handle("/mcp", server.HTTPHandler())
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		})
		srv := &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			// #nosec G706 -- tenantID/boardID are run through sanitizeForLog before interpolation; gosec's taint analysis does not recognize the sanitizer wrapper.
			log.Printf("mcpd: serving HTTP transport on %s (tenant=%s board=%s, auth=%v)", addr, sanitizeForLog(*tenantID), sanitizeForLog(*boardID), token != "")
			if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Fatalf("mcpd: http: %v", err)
			}
		}()
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = srv.Shutdown(shutdownCtx)
	default:
		log.Fatalf("mcpd: unknown transport %q (want stdio or http)", *transport)
	}
}

func seedDefaultCards(adapter *mcp.InMemoryBoardAdapter, tenantID, boardID string) {
	adapter.SeedCards(tenantID, boardID, []board.Card{
		{
			ID:     "mcp-seed-001",
			Title:  "Wire your MCP client to auto-bot",
			Status: board.StatusBacklog,
			Notes:  "Try board.list_cards, then card.create, then card.comment.",
			Tags:   []string{"mcp", "onboarding"},
		},
	})
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func envOrInt(key string, fallback int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
		return fallback
	}
	return n
}

// sanitizeForLog strips control characters from operator-supplied strings
// (env vars, flag values) before they hit the structured log so log forging
// via newline injection is not possible. Closes gosec G706 on the
// log.Printf call sites that interpolate flag values.
func sanitizeForLog(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
