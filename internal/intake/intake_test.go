package intake

import (
	"errors"
	"testing"
	"time"
)

func TestNormalize_TrimsAndStampsTime(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	in := Intake{
		Submitter: "  daria  ",
		Yesterday: "  shipped X  ",
		Today:     "  continue Y  ",
		Blockers: []BlockerItem{
			{Text: "  need Linear creds  "},
			{Text: ""},
		},
		MentionedCards: []string{"card-007", " ", "card-007", "card-008"},
	}
	out, err := Normalize(in, now)
	if err != nil {
		t.Fatalf("Normalize returned error: %v", err)
	}
	if out.Submitter != "daria" {
		t.Errorf("Submitter not trimmed: %q", out.Submitter)
	}
	if out.Yesterday != "shipped X" || out.Today != "continue Y" {
		t.Errorf("Yesterday/Today not trimmed: %q / %q", out.Yesterday, out.Today)
	}
	if len(out.Blockers) != 1 || out.Blockers[0].Text != "need Linear creds" {
		t.Errorf("Blockers not cleaned: %+v", out.Blockers)
	}
	wantCards := []string{"card-007", "card-008"}
	if len(out.MentionedCards) != len(wantCards) {
		t.Fatalf("MentionedCards not deduped: %+v", out.MentionedCards)
	}
	for i, w := range wantCards {
		if out.MentionedCards[i] != w {
			t.Errorf("MentionedCards[%d] = %q, want %q", i, out.MentionedCards[i], w)
		}
	}
	if !out.SubmittedAt.Equal(now) {
		t.Errorf("SubmittedAt not stamped: %v", out.SubmittedAt)
	}
	if out.Source != SourceForm {
		t.Errorf("Source default = %q, want form", out.Source)
	}
}

func TestNormalize_RejectsEmpty(t *testing.T) {
	now := time.Now()
	_, err := Normalize(Intake{Submitter: "daria"}, now)
	if !errors.Is(err, ErrEmptyIntake) {
		t.Fatalf("Normalize on empty body: want ErrEmptyIntake, got %v", err)
	}
}

func TestNormalize_RejectsMissingSubmitter(t *testing.T) {
	now := time.Now()
	_, err := Normalize(Intake{Today: "ship X"}, now)
	if !errors.Is(err, ErrMissingSubmitter) {
		t.Fatalf("Normalize without submitter: want ErrMissingSubmitter, got %v", err)
	}
}

func TestNormalize_PreservesSubmittedAt(t *testing.T) {
	supplied := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	out, err := Normalize(Intake{
		Submitter:   "daria",
		Today:       "x",
		SubmittedAt: supplied,
	}, now)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if !out.SubmittedAt.Equal(supplied) {
		t.Errorf("SubmittedAt overwritten: got %v, want %v", out.SubmittedAt, supplied)
	}
}

func TestParse_HeadersSplit(t *testing.T) {
	body := `Yesterday: shipped the IPv6 thing (card-007)
Today: continue auth refactor
Blockers:
- need Linear creds
- waiting on PROJ-42`
	got := NewHeuristicParser().Parse(body)
	if got.Yesterday != "shipped the IPv6 thing (card-007)" {
		t.Errorf("Yesterday=%q", got.Yesterday)
	}
	if got.Today != "continue auth refactor" {
		t.Errorf("Today=%q", got.Today)
	}
	if len(got.Blockers) != 2 {
		t.Fatalf("Blockers count = %d: %+v", len(got.Blockers), got.Blockers)
	}
	if got.Blockers[0].Text != "need Linear creds" {
		t.Errorf("Blockers[0].Text=%q", got.Blockers[0].Text)
	}
	if got.Blockers[1].CardID != "PROJ-42" {
		t.Errorf("Blockers[1].CardID=%q, want PROJ-42", got.Blockers[1].CardID)
	}
	// MentionedCards should cover card-007 (from Yesterday) AND PROJ-42
	// (lifted from blocker line).
	found := map[string]bool{}
	for _, c := range got.MentionedCards {
		found[c] = true
	}
	if !found["card-007"] || !found["PROJ-42"] {
		t.Errorf("MentionedCards missing expected refs: %+v", got.MentionedCards)
	}
}

func TestParse_FreeFormFallsBackToToday(t *testing.T) {
	got := NewHeuristicParser().Parse("Just gonna keep going on the auth refactor today, no blockers.")
	if got.Yesterday != "" {
		t.Errorf("Yesterday should be empty: %q", got.Yesterday)
	}
	if got.Today == "" {
		t.Errorf("Today should fall back to whole body, got empty")
	}
	if len(got.Blockers) != 0 {
		t.Errorf("Unexpected blockers: %+v", got.Blockers)
	}
}

func TestParse_SlackBracketTemplate(t *testing.T) {
	body := `[yesterday] shipped the IPv6 thing
[today] continue on auth refactor
[blockers] need Linear creds`
	got := ParseSlackTemplate(body)
	if got.Yesterday != "shipped the IPv6 thing" {
		t.Errorf("Yesterday=%q", got.Yesterday)
	}
	if got.Today != "continue on auth refactor" {
		t.Errorf("Today=%q", got.Today)
	}
	if len(got.Blockers) != 1 || got.Blockers[0].Text != "need Linear creds" {
		t.Errorf("Blockers=%+v", got.Blockers)
	}
}

func TestVerifySlackSignature_OK(t *testing.T) {
	secret := "shhh"
	body := []byte(`{"hello":"world"}`)
	now := time.Now()
	ts := now.Add(-30 * time.Second)
	tsStr := unixSecondsString(ts)
	sig := ComputeSlackSignature(secret, tsStr, body)
	if err := VerifySlackSignature(secret, tsStr, body, sig, now); err != nil {
		t.Fatalf("VerifySlackSignature: %v", err)
	}
}

func TestVerifySlackSignature_MissingSecret(t *testing.T) {
	if err := VerifySlackSignature("", "1234", nil, "v0=00", time.Now()); !errors.Is(err, ErrSlackSecretMissing) {
		t.Fatalf("want ErrSlackSecretMissing, got %v", err)
	}
}

func TestVerifySlackSignature_StaleTimestamp(t *testing.T) {
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	stale := now.Add(-10 * time.Minute)
	tsStr := unixSecondsString(stale)
	sig := ComputeSlackSignature("shhh", tsStr, []byte("body"))
	if err := VerifySlackSignature("shhh", tsStr, []byte("body"), sig, now); !errors.Is(err, ErrSlackTimestampStale) {
		t.Fatalf("want ErrSlackTimestampStale, got %v", err)
	}
}

func TestVerifySlackSignature_Mismatch(t *testing.T) {
	now := time.Now()
	tsStr := unixSecondsString(now)
	// Sign with one secret, verify with another — must reject.
	sig := ComputeSlackSignature("attacker-secret", tsStr, []byte("body"))
	if err := VerifySlackSignature("real-secret", tsStr, []byte("body"), sig, now); !errors.Is(err, ErrSlackSignatureMismatch) {
		t.Fatalf("want ErrSlackSignatureMismatch, got %v", err)
	}
}

func TestVerifySlackSignature_BadTimestampFormat(t *testing.T) {
	if err := VerifySlackSignature("shhh", "not-a-number", nil, "v0=00", time.Now()); !errors.Is(err, ErrSlackTimestampInvalid) {
		t.Fatalf("want ErrSlackTimestampInvalid, got %v", err)
	}
	if err := VerifySlackSignature("shhh", "", nil, "v0=00", time.Now()); !errors.Is(err, ErrSlackTimestampInvalid) {
		t.Fatalf("blank timestamp: want ErrSlackTimestampInvalid, got %v", err)
	}
}

func TestMemoryStore_RecentWindow(t *testing.T) {
	store := NewMemoryStore(0)
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	old := Intake{TenantID: "t1", BoardID: "b1", Submitter: "old", Today: "x", SubmittedAt: now.Add(-48 * time.Hour)}
	mid := Intake{TenantID: "t1", BoardID: "b1", Submitter: "mid", Today: "y", SubmittedAt: now.Add(-6 * time.Hour)}
	fresh := Intake{TenantID: "t1", BoardID: "b1", Submitter: "fresh", Today: "z", SubmittedAt: now.Add(-1 * time.Hour)}
	store.Put(old)
	store.Put(mid)
	store.Put(fresh)

	recent := store.Recent("t1", "b1", now.Add(-24*time.Hour))
	if len(recent) != 2 {
		t.Fatalf("Recent count = %d: %+v", len(recent), recent)
	}
	// Newest-first
	if recent[0].Submitter != "fresh" || recent[1].Submitter != "mid" {
		t.Errorf("Recent ordering wrong: %+v", recent)
	}
}

func TestMemoryStore_PerBoardCapEvictsOldest(t *testing.T) {
	store := NewMemoryStore(2)
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
	store.Put(Intake{TenantID: "t1", BoardID: "b1", Submitter: "a", Today: "x", SubmittedAt: now.Add(-3 * time.Hour)})
	store.Put(Intake{TenantID: "t1", BoardID: "b1", Submitter: "b", Today: "x", SubmittedAt: now.Add(-2 * time.Hour)})
	store.Put(Intake{TenantID: "t1", BoardID: "b1", Submitter: "c", Today: "x", SubmittedAt: now.Add(-1 * time.Hour)})
	all := store.All("t1", "b1")
	if len(all) != 2 {
		t.Fatalf("cap not enforced: %d entries", len(all))
	}
	names := []string{all[0].Submitter, all[1].Submitter}
	if names[0] == "a" || names[1] == "a" {
		t.Errorf("oldest entry was not evicted: %+v", names)
	}
}

func TestMemoryStore_TenantIsolation(t *testing.T) {
	store := NewMemoryStore(0)
	now := time.Now()
	store.Put(Intake{TenantID: "t1", BoardID: "b1", Submitter: "alice", Today: "a", SubmittedAt: now})
	store.Put(Intake{TenantID: "t2", BoardID: "b1", Submitter: "bob", Today: "b", SubmittedAt: now})
	t1 := store.Recent("t1", "b1", now.Add(-1*time.Hour))
	if len(t1) != 1 || t1[0].Submitter != "alice" {
		t.Errorf("Tenant t1 view leaked: %+v", t1)
	}
}

func unixSecondsString(t time.Time) string {
	return itoa(t.Unix())
}

// itoa avoids strconv to keep test isolation; identical output to
// strconv.FormatInt(n, 10).
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
