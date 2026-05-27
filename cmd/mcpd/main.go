// Command mcpd exposes the auto-bot kanban over the Model Context Protocol
// so any LLM client (Claude Code, Cursor, Claude Agent SDK scripts) can
// read and mutate the board via the same audit + risk-classification path
// the voice tools use.
//
// Sprint 2.1 status: MCP tools route through cmd/server's
// /internal/tools/dispatch endpoint, so every MCP-driven mutation flows
// through ApplyToolCall — the audit log (action_replay_events), risk
// classification, and confirmation gates apply uniformly to voice, UI,
// and MCP callers.
//
// When --board-url is empty, mcpd falls back to an in-memory mock board.
// This keeps the foundational slice runnable in standalone tests and
// examples; production deployments always set --board-url + BOARD_TOKEN.
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
		boardURL  = flag.String("board-url", envOr("BOARD_URL", ""), "cmd/server base URL (e.g. http://app:3000); empty falls back to in-memory mock")
	)
	flag.Parse()

	// #58 hard cut: MCPD_TOKEN is gone. HTTP callers must present a signed
	// token (POST /admin/mcp-tokens on cmd/server); the keys to verify
	// those tokens are loaded from MCP_SIGNING_KEYS at boot. Missing keys
	// for the http transport is a hard error — fail-closed means the
	// process refuses to come up rather than serving anonymous requests.
	var verifier *mcp.Verifier
	if *transport == "http" {
		rawKeys := strings.TrimSpace(os.Getenv("MCP_SIGNING_KEYS"))
		if rawKeys == "" {
			log.Fatalf("mcpd: MCP_SIGNING_KEYS is required for HTTP transport; refusing to serve anonymous requests")
		}
		keys, err := mcp.ParseSigningKeys(rawKeys)
		if err != nil {
			log.Fatalf("mcpd: MCP_SIGNING_KEYS invalid: %v", err)
		}
		verifier, err = mcp.NewVerifier(keys, mcp.NewMemoryReplayTracker())
		if err != nil {
			log.Fatalf("mcpd: NewVerifier: %v", err)
		}
	}

	// Stdio callers cross only the OS process boundary, so the trust
	// model is the local OS. The operator declares the scopes the stdio
	// caller gets via MCPD_STDIO_SCOPES (comma-separated). Omitted →
	// the full set, since the operator is the only one with stdio.
	stdioScopes := parseStdioScopes(os.Getenv("MCPD_STDIO_SCOPES"))

	boardToken := os.Getenv("BOARD_TOKEN")
	var client mcp.BoardClient
	if strings.TrimSpace(*boardURL) != "" {
		if boardToken == "" {
			log.Println("mcpd: WARNING — BOARD_URL is set but BOARD_TOKEN is empty; cmd/server will reject dispatches")
		}
		client = mcp.NewHTTPBoardClient(*boardURL, boardToken, "mcp")
		// #nosec G706 -- boardURL, tenantID, boardID are run through sanitizeForLog before interpolation; gosec's taint analysis does not recognize the sanitizer wrapper.
		log.Printf("mcpd: routing tools to cmd/server at %s (tenant=%s board=%s)", sanitizeForLog(*boardURL), sanitizeForLog(*tenantID), sanitizeForLog(*boardID))
	} else {
		fallback := mocks.NewBoardClient()
		seedDefaultCards(fallback, *tenantID, *boardID)
		client = fallback
		// #nosec G706 -- tenantID and boardID are run through sanitizeForLog before interpolation; gosec's taint analysis does not recognize the sanitizer wrapper.
		log.Printf("mcpd: BOARD_URL is empty; running with in-memory mock (tenant=%s board=%s)", sanitizeForLog(*tenantID), sanitizeForLog(*boardID))
	}

	runStore := mocks.NewRunStore()
	coordinator := agent.NewSimpleRunCoordinator(runStore, nil)

	tools := mcp.BuildTools(mcp.ToolDeps{
		Board:        client,
		RunStore:     runStore,
		Coordinator:  coordinator,
		TenantID:     *tenantID,
		BoardID:      *boardID,
		DefaultActor: "mcp",
	})
	server := mcp.NewServer(tools)
	server.Verifier = verifier
	// StdioClaims is only meaningful for the stdio transport — HTTP path
	// reads claims from the bearer. Leave it zero for HTTP runs so the
	// stdio default scope list does not silently shadow a misconfigured
	// HTTP deployment.
	if strings.ToLower(*transport) == "stdio" {
		server.StdioClaims = mcp.Claims{
			Subject:  "stdio:" + *tenantID,
			TenantID: *tenantID,
			Scopes:   stdioScopes,
		}
	}

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
			// #nosec G706 -- addr is fmt.Sprintf(":%d", *port) (digits only); tenantID and boardID are run through sanitizeForLog. gosec's taint analysis does not recognize the sanitizer wrapper.
			log.Printf("mcpd: serving HTTP transport on %s (tenant=%s board=%s, signed_tokens=true)", addr, sanitizeForLog(*tenantID), sanitizeForLog(*boardID))
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

// parseStdioScopes splits a comma-separated MCPD_STDIO_SCOPES into the
// canonical scope slice. Empty input defaults to the full tool-scope
// set so an operator running stdio locally doesn't have to think about
// scopes at all (the trust boundary is the OS process). Explicit empty
// list (a single comma) yields an empty scope slice, which the centralized
// scope check will then reject for any gated tool.
func parseStdioScopes(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return mcp.AllToolScopes()
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func seedDefaultCards(client *mocks.BoardClient, tenantID, boardID string) {
	client.SeedCards(tenantID, boardID, []board.Card{
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
// via newline injection is not possible.
func sanitizeForLog(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}
