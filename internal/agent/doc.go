// Package agent owns durable agent Runs: the state machine, checkpoints,
// human-clarification questions, cost accounting, and the RunCoordinator
// interface that schedules and resumes runs.
//
// A Run is bound to a board Card. It records plan steps, evidence, and
// progress on the card thread so humans and agents share a single
// communication surface.
//
// Sprint 0 status: skeleton package. The existing agentRun struct in
// cmd/server/agent_runs.go is moved here in Sprint 0.3. The RunCoordinator
// interface and ask-the-human flow land in Sprint 1.
package agent
