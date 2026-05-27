package main

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/somoore/auto-bot/internal/standup"
)

// cronTickInterval is the cadence of the lightweight scheduler goroutine.
// It is a small fixed interval rather than a cron daemon; tasks that need
// less frequent execution self-rate-limit via lastRunAt timestamps.
const cronTickInterval = 60 * time.Second

// agendaRefreshInterval is how often the scheduler runs BuildAgenda for each
// active board. 15 minutes balances voice-meeting freshness against the cost
// of repeated reads — the agenda is small so even 5 minutes would be fine.
const agendaRefreshInterval = 15 * time.Minute

// cronScheduler is a singleton goroutine that fires periodic background
// tasks (today: pre-meeting agenda builds; later: pending-action sweeper,
// cost rollups). Start is idempotent; Stop cancels the context and waits
// for the goroutine to exit.
type cronScheduler struct {
	ctx       context.Context
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	board     *kanbanBoard
	builder   *standup.AgendaBuilder
	lastBuilt time.Time
	lastMu    sync.Mutex
}

var (
	cron     *cronScheduler
	cronOnce sync.Once
)

// startCronScheduler installs the singleton scheduler and starts the
// background goroutine. Safe to call multiple times; subsequent calls are
// no-ops. The supplied parent context becomes the cancellation parent so
// the goroutine exits cleanly on shutdown.
func startCronScheduler(parent context.Context, b *kanbanBoard) {
	cronOnce.Do(func() {
		ctx, cancel := context.WithCancel(parent)
		cron = &cronScheduler{
			ctx:     ctx,
			cancel:  cancel,
			board:   b,
			builder: agendaBuilderFor(b),
		}
		cron.wg.Add(1)
		go cron.run()
	})
}

// stopCronScheduler cancels the background goroutine and waits for it to
// exit. It is safe to call when the scheduler was never started.
func stopCronScheduler() {
	if cron == nil {
		return
	}
	cron.cancel()
	cron.wg.Wait()
}

func (s *cronScheduler) run() {
	defer s.wg.Done()
	ticker := time.NewTicker(cronTickInterval)
	defer ticker.Stop()
	// Run the first tick immediately so cold-start sessions get an agenda
	// without waiting cronTickInterval.
	s.tick()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.tick()
		}
	}
}

func (s *cronScheduler) tick() {
	s.maybeBuildAgenda()
	s.sweepPendingActions()
}

// maybeBuildAgenda rate-limits to agendaRefreshInterval and stores the
// freshest agenda on the scheduler so the Nova Sonic init sequence can pick
// it up without re-running BuildAgenda on every voice session. The agenda is
// also broadcast as a `pre_meeting_agenda` WS event so the React drawer can
// render it ahead of the next standup.
func (s *cronScheduler) maybeBuildAgenda() {
	if s.builder == nil || s.board == nil {
		return
	}
	s.lastMu.Lock()
	since := time.Since(s.lastBuilt)
	s.lastMu.Unlock()
	if since < agendaRefreshInterval {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 10*time.Second)
	defer cancel()
	agenda, err := s.builder.BuildAgenda(ctx, s.board.tenantID, s.board.boardID, 0)
	if err != nil {
		log.Warnf("cron agenda build: %v", err)
		return
	}
	s.lastMu.Lock()
	s.lastBuilt = time.Now().UTC()
	s.lastMu.Unlock()
	broadcastKanbanEventForBoard(s.board.tenantID, s.board.boardID, "pre_meeting_agenda", agenda)
	if summary := strings.TrimSpace(agenda.Summary); summary != "" {
		log.Infof("cron: refreshed standup agenda (%s)", summary)
	}
}

// sweepPendingActions marks past-deadline dry-run actions as expired so the
// queue does not accumulate orphans. The action store is the source of
// truth; the WS broadcast keeps connected UIs in sync.
func (s *cronScheduler) sweepPendingActions() {
	store := globalPendingActionStore()
	if store == nil || s.board == nil {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	expired, err := store.ExpirePendingActions(ctx, s.board.tenantID, s.board.boardID, time.Now().UTC())
	if err != nil {
		log.Warnf("cron pending-action sweep: %v", err)
		return
	}
	if expired > 0 {
		log.Infof("cron: expired %d pending action(s)", expired)
		broadcastKanbanEventForBoard(s.board.tenantID, s.board.boardID, "pending_actions_expired", map[string]any{"count": expired})
	}
}
