// Package mcp implements the Model Context Protocol server surface for
// auto-bot. External LLM clients (Claude Code, Cursor, Claude Agent SDK
// scripts) connect via stdio or HTTP and call tools that read and mutate
// the board, drive Runs, and answer human-clarification questions.
//
// All MCP tool calls route through the same RunCoordinator and audit log
// (action_replay_events) as voice tool calls — the audit and guardrail
// story is identical.
//
// Sprint 0 status: skeleton. The MCP surface lands in Sprint 2.
package mcp
