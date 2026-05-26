// Package meetings owns the canonical scrum, transcript, and confirmation
// data types shared across cmd/server, the meeting intelligence reporter,
// and the board store.
//
// These are pure data shapes: scrum meeting state, participants and updates,
// retained transcript entries, host-confirmation prompts, external action
// receipts, and the client-safe board mutation view. Operational logic
// (recordMutation, generateScrumBriefing, etc.) stays in cmd/server.
//
// Sprint 1.0a status: types extracted; cmd/server keeps aliases so all
// existing code continues to refer to the local names. Sprint 1.1 will move
// the composite kanbanBoardState / kanbanActor types into internal/board on
// top of these stable scrum types.
package meetings
