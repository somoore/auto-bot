// Package standup owns the closed-loop standup workflow:
//
//   - BuildAgenda assembles a pre-meeting agenda from yesterday's runs,
//     unresolved blockers, and cards awaiting review.
//   - Close finalizes a meeting by creating follow-up cards, kicking off
//     agent Runs for agent-assigned work, and persisting the meeting
//     report as a typed artifact.
//
// Sprint 0 status: skeleton. The agenda and closer land in Sprint 4.
package standup
