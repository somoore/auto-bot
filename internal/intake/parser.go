package intake

import (
	"regexp"
	"strings"
)

// Parser converts a free-form intake body into a structured Intake. The
// default implementation is a stdlib-only heuristic; cmd/server can inject
// a Bedrock-backed Parser when it wants LLM-assisted extraction for the
// SourceAPI free-text path.
type Parser interface {
	// Parse turns rawText into a partial Intake. Submitter, TenantID,
	// BoardID, and Source are caller-controlled and are NOT filled by the
	// parser — Parse only extracts the message body (yesterday/today/
	// blockers/mentioned cards). The caller stitches the rest in before
	// calling Normalize.
	Parse(rawText string) Intake
}

// HeuristicParser is the default Parser. It looks for "yesterday:",
// "today:", and "blockers:" headers (case-insensitive, with optional
// markdown bullets / brackets) and falls back to using the whole body as
// Today when no headers match. It also pulls card references that look
// like "card-001" or "PROJ-42" out of the body into MentionedCards.
type HeuristicParser struct{}

// NewHeuristicParser constructs a HeuristicParser. The zero value works;
// the constructor exists for symmetry with future LLM-backed parsers.
func NewHeuristicParser() HeuristicParser {
	return HeuristicParser{}
}

// sectionRe matches a single line that opens a yesterday/today/blockers
// section. The capture group is the header keyword. Anchored to the start
// of a line so "today: ship X" matches but "by today: ship X" does not.
// sectionRe matches a section header. Two forms are accepted:
//
//   - Markdown-style: "Yesterday: shipped X" or "- today - did Y".
//   - Slack-template style: "[yesterday] shipped X" (no separator).
//
// In the bracket form the separator is optional; in the bare-word form
// either ":" or "-" is required so we don't accidentally promote
// "yesterday I shipped X" into a section opener.
var sectionRe = regexp.MustCompile(`(?im)^\s*(?:[-*]\s*)?(?:\[(yesterday|today|blockers?|blocked\s*on)\]\s*[:\-]?\s*(.*)|(yesterday|today|blockers?|blocked\s*on)\s*[:\-]\s*(.*))$`)

// cardRefRe pulls likely card identifiers out of text. Matches the
// auto-bot "card-001" form and standard Jira/Linear "PROJ-42" forms. Kept
// permissive on purpose — false positives are filtered by the board when
// the references are resolved into actual cards.
var cardRefRe = regexp.MustCompile(`\b(card-\d+|[A-Z][A-Z0-9]{1,9}-\d+)\b`)

// Parse implements Parser.
func (HeuristicParser) Parse(rawText string) Intake {
	out := Intake{RawText: rawText}
	body := strings.TrimSpace(rawText)
	if body == "" {
		return out
	}

	sections := splitSections(body)
	out.Yesterday = strings.TrimSpace(sections["yesterday"])
	out.Today = strings.TrimSpace(sections["today"])

	if blockersBlock, ok := sections["blockers"]; ok {
		out.Blockers = parseBlockerLines(blockersBlock)
	}

	if out.Yesterday == "" && out.Today == "" && len(out.Blockers) == 0 {
		// No headers — treat the whole message as a today summary.
		out.Today = body
	}

	out.MentionedCards = extractCardRefs(body)
	// Also surface card_id values that appeared inside blocker lines
	// (e.g. "blocked on card-007: waiting for review").
	for _, b := range out.Blockers {
		if b.CardID == "" {
			continue
		}
		if !containsString(out.MentionedCards, b.CardID) {
			out.MentionedCards = append(out.MentionedCards, b.CardID)
		}
	}

	return out
}

// splitSections walks the text line by line, tracking the most recently
// opened header. Lines that don't open a new section are appended to the
// active section. Returns a map keyed by canonical lowercased section name
// ("yesterday", "today", "blockers"). The "blocked on" variant is
// canonicalized to "blockers".
func splitSections(body string) map[string]string {
	sections := map[string]string{}
	current := ""
	var builder strings.Builder

	flush := func() {
		if current == "" {
			return
		}
		sections[current] = strings.TrimSpace(builder.String())
		builder.Reset()
	}

	for _, line := range strings.Split(body, "\n") {
		match := sectionRe.FindStringSubmatch(line)
		if match != nil {
			flush()
			// The regex has two alternation arms; whichever matched
			// fills (header, rest) in either slots [1],[2] or [3],[4].
			header := match[1]
			rest := match[2]
			if header == "" {
				header = match[3]
				rest = match[4]
			}
			current = canonSection(header)
			builder.WriteString(rest)
			builder.WriteString("\n")
			continue
		}
		if current != "" {
			builder.WriteString(line)
			builder.WriteString("\n")
		}
	}
	flush()
	return sections
}

func canonSection(raw string) string {
	r := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.HasPrefix(r, "blocker"), strings.HasPrefix(r, "blocked"):
		return "blockers"
	case r == "yesterday":
		return "yesterday"
	case r == "today":
		return "today"
	default:
		return r
	}
}

// parseBlockerLines turns a multi-line blockers block into structured
// BlockerItem records. Bullet markers (-, *, •, digits.) are stripped.
// Blank lines and pure-whitespace lines are dropped. If a line contains a
// card reference, that reference is lifted into BlockerItem.CardID.
func parseBlockerLines(block string) []BlockerItem {
	var items []BlockerItem
	for _, line := range strings.Split(block, "\n") {
		line = stripBullet(line)
		if line == "" {
			continue
		}
		item := BlockerItem{Text: line}
		if ref := cardRefRe.FindString(line); ref != "" {
			item.CardID = ref
		}
		items = append(items, item)
	}
	return items
}

var bulletPrefixRe = regexp.MustCompile(`^\s*(?:[-*•]|\d+[.)])\s+`)

func stripBullet(line string) string {
	line = strings.TrimRight(line, " \t\r")
	line = bulletPrefixRe.ReplaceAllString(line, "")
	return strings.TrimSpace(line)
}

func extractCardRefs(body string) []string {
	matches := cardRefRe.FindAllString(body, -1)
	if len(matches) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		if _, dup := seen[m]; dup {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// ParseSlackTemplate parses the standard Slack standup template:
//
//	[yesterday] shipped the IPv6 thing
//	[today] continue on auth refactor
//	[blockers] need Linear creds; waiting on card-007
//
// Blockers are split on ";" so a single Slack line can carry multiple
// items. Falls back to the HeuristicParser if no template tokens match,
// so a malformed Slack post still yields something useful.
func ParseSlackTemplate(rawText string) Intake {
	return NewHeuristicParser().Parse(rawText)
}
