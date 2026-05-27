import { describe, it, expect, beforeEach, afterEach, vi } from "vitest"
import { render, screen, fireEvent, waitFor } from "@testing-library/react"
import { IntakeForm } from "./IntakeForm"

// IntakeForm tests cover the three states the user sees:
//   1. submitting an empty form is rejected without a network call,
//   2. a successful submit renders the confirmation block listing
//      created cards + posted comments,
//   3. a server-side error renders the error message and clears no
//      fields (so the user can correct + retry).

const SUCCESS_PAYLOAD = {
  ok: true,
  intake: { submitter: "daria", today: "ship auth", submitted_at: "2026-05-26T12:00:00Z" },
  created: [{ id: "card-100", title: "need Linear creds", status: "Blocked" }],
  comments: [{ card_id: "card-001", body: "Async intake from daria...", author: "daria" }],
}

describe("IntakeForm", () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it("blocks submit with no content and surfaces a local error", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(SUCCESS_PAYLOAD), { status: 200 }),
    )
    render(<IntakeForm />)
    fireEvent.click(screen.getByTestId("intake-submit"))
    expect(await screen.findByTestId("intake-error")).toHaveTextContent(
      /at least a yesterday/i,
    )
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it("submits to /intake/standup and renders the confirmation", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(SUCCESS_PAYLOAD), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
    )
    render(<IntakeForm />)

    fireEvent.change(screen.getByTestId("intake-today"), {
      target: { value: "working on auth refactor" },
    })
    fireEvent.change(screen.getByTestId("intake-blockers"), {
      target: { value: "need Linear creds" },
    })
    fireEvent.click(screen.getByTestId("intake-submit"))

    await waitFor(() => {
      expect(screen.getByTestId("intake-confirmation")).toBeInTheDocument()
    })
    expect(fetchSpy).toHaveBeenCalledTimes(1)
    const [url, init] = fetchSpy.mock.calls[0]!
    expect(url).toBe("/intake/standup")
    expect(init?.method).toBe("POST")
    const body = JSON.parse((init?.body as string) ?? "{}")
    expect(body).toEqual({
      yesterday: "",
      today: "working on auth refactor",
      blockers: [{ text: "need Linear creds" }],
      source: "form",
    })

    // Confirmation includes the created card title and the comment
    // target so the user can verify what changed.
    expect(screen.getByTestId("intake-created-list")).toHaveTextContent(
      "need Linear creds",
    )
    expect(screen.getByTestId("intake-comments-list")).toHaveTextContent("card-001")
  })

  it("surfaces a server error and keeps fields populated for retry", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ error: "submitter is required" }), {
        status: 400,
        headers: { "Content-Type": "application/json" },
      }),
    )
    render(<IntakeForm />)
    fireEvent.change(screen.getByTestId("intake-today"), {
      target: { value: "doing the thing" },
    })
    fireEvent.click(screen.getByTestId("intake-submit"))

    expect(await screen.findByTestId("intake-error")).toHaveTextContent(
      /submitter is required/i,
    )
    // Today field NOT cleared so the user can edit and retry.
    expect(screen.getByTestId("intake-today")).toHaveValue("doing the thing")
    // No confirmation block on failure.
    expect(screen.queryByTestId("intake-confirmation")).toBeNull()
  })

  it("attaches a bearer token header when configured", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(SUCCESS_PAYLOAD), { status: 200 }),
    )
    render(<IntakeForm bearerToken="test-token" />)
    fireEvent.change(screen.getByTestId("intake-today"), {
      target: { value: "x" },
    })
    fireEvent.click(screen.getByTestId("intake-submit"))
    await waitFor(() => expect(fetchSpy).toHaveBeenCalled())
    const [, init] = fetchSpy.mock.calls[0]!
    const headers = (init?.headers ?? {}) as Record<string, string>
    expect(headers["Authorization"]).toBe("Bearer test-token")
  })

  it("parses one blocker per non-blank line", async () => {
    const fetchSpy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify(SUCCESS_PAYLOAD), { status: 200 }),
    )
    render(<IntakeForm />)
    fireEvent.change(screen.getByTestId("intake-blockers"), {
      target: { value: "need Linear creds\n\nwaiting on PR review\n   " },
    })
    fireEvent.click(screen.getByTestId("intake-submit"))
    await waitFor(() => expect(fetchSpy).toHaveBeenCalled())
    const [, init] = fetchSpy.mock.calls[0]!
    const body = JSON.parse((init?.body as string) ?? "{}")
    expect(body.blockers).toEqual([
      { text: "need Linear creds" },
      { text: "waiting on PR review" },
    ])
  })
})
