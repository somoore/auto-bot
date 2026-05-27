package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/somoore/auto-bot/internal/intake"
)

// intakeRecentWindow is the lookback Recent uses for GET /intake/standup.
// Daria's persona feedback explicitly anchors async standups at "the last
// 24h" — a single sleep cycle for an EM picking up the world the next
// morning. Longer windows surface stale signal; shorter windows miss
// engineers in different time zones.
const intakeRecentWindow = 24 * time.Hour

// intakeStore holds the in-memory async-standup buffer. main() injects a
// MemoryStore at startup; tests can swap in their own intake.Store.
var intakeStore intake.Store = intake.NewMemoryStore(200)

// intakeParser is the parser used for SourceAPI / SourceSlack free-form
// bodies. Default is the heuristic parser; tests can override.
var intakeParser intake.Parser = intake.NewHeuristicParser()

// slackSigningSecret is the HMAC secret for the Slack webhook adapter.
// Loaded from the SLACK_SIGNING_SECRET env at startup. Empty means the
// /intake/slack endpoint will reject every request — the constraint is
// "safe defaults that reject if unset".
var slackSigningSecret string

// configureIntakeFromEnv reads SLACK_SIGNING_SECRET into the package
// global. Called from main during startup. Kept as a separate function
// so tests can re-run it after manipulating env vars.
func configureIntakeFromEnv() {
	slackSigningSecret = strings.TrimSpace(os.Getenv("SLACK_SIGNING_SECRET"))
}

// intakeStandupHandler routes POST and GET /intake/standup.
//
//   - POST accepts either an intake.Intake JSON body OR a free-form text
//     body (Content-Type: text/plain). For SourceAPI / SourceSlack with a
//     populated RawText (and no structured fields), intakeParser fills
//     yesterday/today/blockers/mentioned_cards from the raw text. For
//     SourceForm, the structured body is passed through.
//   - GET returns intakeStore.Recent for the caller's tenant + board
//     over the last 24h, newest-first.
//
// Auth: same Bearer / session-cookie check as the rest of cmd/server.
// The caller's identity is used as the submitter only when the request
// body leaves submitter blank — keep the form free to record a third
// party's standup if needed (an EM filing a standup on behalf of a
// teammate is a legitimate flow).
func intakeStandupHandler(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w)
	ctx, ok := authorizeBaseRequest(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodPost:
		handleIntakePost(w, r, ctx)
	case http.MethodGet:
		handleIntakeGet(w, ctx)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func handleIntakePost(w http.ResponseWriter, r *http.Request, ctx requestAuthContext) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeIntakeError(w, http.StatusBadRequest, fmt.Sprintf("read body: %v", err))
		return
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		writeIntakeError(w, http.StatusBadRequest, "empty body")
		return
	}

	in, err := decodeIntakeBody(r.Header.Get("Content-Type"), body)
	if err != nil {
		writeIntakeError(w, http.StatusBadRequest, err.Error())
		return
	}

	in = stitchIntakeIdentity(in, ctx)
	in = autoParseIfNeeded(in)

	normalized, err := intake.Normalize(in, time.Now())
	if err != nil {
		writeIntakeError(w, http.StatusBadRequest, err.Error())
		return
	}

	stored := intakeStore.Put(normalized)

	// Fan out card creation + comments. The caller identity is passed
	// in so runIntakeFollowups can decide whether assign_ticket
	// confirmation should be skipped (self-assign) or queued
	// (EM-files-on-behalf path; SecArch-002).
	followups := runIntakeFollowups(stored, ctx.Identity)

	writeIntakeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"intake":   stored,
		"created":  followups.CreatedCards,
		"comments": followups.PostedComments,
	})
}

func handleIntakeGet(w http.ResponseWriter, ctx requestAuthContext) {
	since := time.Now().Add(-intakeRecentWindow)
	intakes := intakeStore.Recent(ctx.TenantID, ctx.BoardID, since)
	writeIntakeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"intakes": intakes,
		"window":  intakeRecentWindow.String(),
	})
}

// decodeIntakeBody handles both the JSON envelope and free-form text
// bodies. JSON is the documented path; text/plain support exists so a
// Slack slash command or shell pipeline can post a standup without
// formatting it first.
func decodeIntakeBody(contentType string, body []byte) (intake.Intake, error) {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch ct {
	case "text/plain", "":
		// If the body starts with { we treat it as JSON anyway so callers
		// that forget the Content-Type header still work.
		if strings.HasPrefix(strings.TrimSpace(string(body)), "{") {
			return decodeIntakeJSON(body)
		}
		return intake.Intake{
			Source:  intake.SourceAPI,
			RawText: string(body),
		}, nil
	case "application/json":
		return decodeIntakeJSON(body)
	default:
		// Unknown — try JSON, then fall back to plain text. Strict 415
		// would needlessly reject legitimate clients posting structured
		// JSON without the canonical media type.
		if in, err := decodeIntakeJSON(body); err == nil {
			return in, nil
		}
		return intake.Intake{
			Source:  intake.SourceAPI,
			RawText: string(body),
		}, nil
	}
}

func decodeIntakeJSON(body []byte) (intake.Intake, error) {
	var in intake.Intake
	if err := json.Unmarshal(body, &in); err != nil {
		return intake.Intake{}, fmt.Errorf("decode intake JSON: %v", err)
	}
	return in, nil
}

// stitchIntakeIdentity fills missing tenant / board / submitter values
// from the request's auth context. This keeps the JSON shape minimal for
// the React form (it only sends yesterday/today/blockers/source) while
// still producing a fully-anchored Intake.
func stitchIntakeIdentity(in intake.Intake, ctx requestAuthContext) intake.Intake {
	if strings.TrimSpace(in.TenantID) == "" {
		in.TenantID = ctx.TenantID
	}
	if strings.TrimSpace(in.BoardID) == "" {
		in.BoardID = ctx.BoardID
	}
	if strings.TrimSpace(in.Submitter) == "" {
		in.Submitter = ctx.Identity
	}
	return in
}

// autoParseIfNeeded runs the heuristic parser when the body has only
// raw text and no structured fields. SourceForm is treated as already
// structured (Daria's form posts yesterday/today/blockers separately)
// so passing RawText alongside is a no-op for that path.
func autoParseIfNeeded(in intake.Intake) intake.Intake {
	if strings.TrimSpace(in.RawText) == "" {
		return in
	}
	if in.Source == intake.SourceForm {
		return in
	}
	hasStructure := strings.TrimSpace(in.Yesterday) != "" ||
		strings.TrimSpace(in.Today) != "" ||
		len(in.Blockers) > 0
	if hasStructure {
		return in
	}
	parsed := intakeParser.Parse(in.RawText)
	in.Yesterday = parsed.Yesterday
	in.Today = parsed.Today
	in.Blockers = parsed.Blockers
	// Merge mentioned cards while preserving any caller-supplied ones.
	seen := map[string]struct{}{}
	merged := make([]string, 0, len(in.MentionedCards)+len(parsed.MentionedCards))
	for _, c := range append([]string{}, in.MentionedCards...) {
		if _, dup := seen[c]; !dup {
			seen[c] = struct{}{}
			merged = append(merged, c)
		}
	}
	for _, c := range parsed.MentionedCards {
		if _, dup := seen[c]; !dup {
			seen[c] = struct{}{}
			merged = append(merged, c)
		}
	}
	in.MentionedCards = merged
	return in
}

// intakeFollowupResult captures what runIntakeFollowups produced. Commit
// 2 returns an empty value; commit 4 fills CreatedCards / PostedComments
// by routing through ApplyToolCallWithMeta.
type intakeFollowupResult struct {
	CreatedCards   []kanbanCard          `json:"created_cards"`
	PostedComments []postedIntakeComment `json:"posted_comments"`
}

// postedIntakeComment is the surface returned to the caller for each
// thread comment posted against a MentionedCards reference. Kept narrow
// so the React form can render a confirmation without re-fetching the
// board.
type postedIntakeComment struct {
	CardID string `json:"card_id"`
	Body   string `json:"body"`
	Author string `json:"author,omitempty"`
}

func writeIntakeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeIntakeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
