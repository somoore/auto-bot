import { describe, expect, it, vi } from "vitest"
import { render, screen, fireEvent } from "@testing-library/react"
import {
  PostMeetingSummary,
  buildRecapText,
  type MeetingReport,
} from "./PostMeetingSummary"

const SAMPLE: MeetingReport = {
  meetingId: "abc",
  title: "Tuesday standup",
  endedAtLabel: "Standup ended · 9:08 AM",
  metadataLabel: "May 26 · 8m 14s · 3 participants + nova",
  breadcrumb: "agent-first v2 / Tuesday standup · summary",
  cardsMoved: 7,
  cardsMovedByNova: 3,
  cardsMovedByTeam: 4,
  runsKickedOff: 2,
  runsEtaMinutes: 24,
  questionsResolved: 1,
  questionAnswer: "per-tenant DB · answered by Scott",
  cost: "$0.43",
  timeSaved: "est. 1h 50m saved",
  decisions: [
    {
      id: "d1",
      title: "Ship per-tenant DB",
      rationale: "Scott confirmed migration path",
      timestamp: "9:04 AM",
    },
  ],
  followUps: [
    { id: "f1", title: "Draft runbook", jiraId: "ABV2-218", assignee: "Daria" },
  ],
  agentsWorking: [
    {
      id: "swe-1",
      name: "swe-1",
      cardId: "ABV2-218",
      stepsDone: 2,
      stepsTotal: 6,
      etaMinutes: 12,
      cost: "$0.12",
    },
  ],
  agentsFootnote: "You'll be notified when either needs your input or completes.",
  syncTargets: [
    { id: "s1", label: "Jira", detail: "7 cards updated", timestamp: "2s ago" },
  ],
}

describe("PostMeetingSummary", () => {
  it("renders the title, metrics, decisions, and follow-ups", () => {
    render(<PostMeetingSummary report={SAMPLE} onBackToBoard={() => {}} />)
    expect(screen.getByTestId("post-meeting-title")).toHaveTextContent(
      "Tuesday standup",
    )
    expect(screen.getByText("Cards moved")).toBeInTheDocument()
    expect(screen.getByText("Agent runs kicked off")).toBeInTheDocument()
    expect(screen.getByText("Ship per-tenant DB")).toBeInTheDocument()
    expect(screen.getByText("ABV2-218")).toBeInTheDocument()
    expect(screen.getByTestId("post-meeting-sync-targets")).toHaveTextContent(
      "Jira",
    )
  })

  it("invokes onBackToBoard when the primary CTA is clicked", () => {
    const onBack = vi.fn()
    render(<PostMeetingSummary report={SAMPLE} onBackToBoard={onBack} />)
    fireEvent.click(screen.getByTestId("post-meeting-back-to-board"))
    expect(onBack).toHaveBeenCalledTimes(1)
  })

  it("copies a formatted recap to the clipboard", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    render(<PostMeetingSummary report={SAMPLE} onBackToBoard={() => {}} />)
    fireEvent.click(screen.getByTestId("post-meeting-copy-recap"))
    expect(writeText).toHaveBeenCalledTimes(1)
    const text = writeText.mock.calls[0][0] as string
    expect(text).toContain("Tuesday standup")
    expect(text).toContain("ABV2-218")
    expect(text).toContain("$0.43")
  })

  it("buildRecapText includes every section", () => {
    const text = buildRecapText(SAMPLE)
    expect(text).toContain("Decisions:")
    expect(text).toContain("Follow-ups:")
    expect(text).toContain("Agents working now:")
    expect(text).toContain("Sync'd to:")
  })
})
