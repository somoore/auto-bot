import { describe, expect, it, vi } from "vitest"
import { fireEvent, render, screen, waitFor } from "@testing-library/react"
import userEvent from "@testing-library/user-event"
import { CardRunTab } from "./CardRunTab"
import { makeCard, makeQuestion, makeRun } from "../test/fixtures"

describe("CardRunTab", () => {
  it("renders the question banner when openRunQuestions has this card", () => {
    render(
      <CardRunTab
        card={makeCard()}
        question={makeQuestion()}
        run={makeRun()}
        dispatch={vi.fn()}
      />,
    )
    expect(
      screen.getByText(/should i retry the failed lint step/i),
    ).toBeInTheDocument()
  })

  it("dispatches run.answer_question when a suggested chip is clicked", async () => {
    const dispatch = vi.fn(async () =>
      ({ ok: true, status: 200, body: undefined }),
    )
    render(
      <CardRunTab
        card={makeCard()}
        question={makeQuestion()}
        run={makeRun()}
        dispatch={dispatch}
        currentUserId="alice"
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: /yes, autofix/i }))
    await waitFor(() => expect(dispatch).toHaveBeenCalledTimes(1))
    expect(dispatch).toHaveBeenCalledWith("run.answer_question", {
      question_id: "q-1",
      answer: "Yes, autofix",
      answered_by: "alice",
      answered_via: "ui",
    })
  })

  it("dispatches agent.take_over_run when Take over is clicked", async () => {
    const dispatch = vi.fn(async () =>
      ({ ok: true, status: 200, body: undefined }),
    )
    render(
      <CardRunTab
        card={makeCard()}
        run={makeRun()}
        dispatch={dispatch}
        currentUserId="alice"
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: /take over/i }))
    await waitFor(() => expect(dispatch).toHaveBeenCalledTimes(1))
    expect(dispatch).toHaveBeenCalledWith("agent.take_over_run", {
      run_id: "run-1",
      taken_over_by: "alice",
    })
  })

  it("renders a collapsed plan summary by default and expands on click", async () => {
    const user = userEvent.setup()
    render(
      <CardRunTab
        card={makeCard()}
        run={makeRun()}
        dispatch={vi.fn()}
      />,
    )
    const plan = screen.getByTestId("plan-list")
    expect(plan).toBeInTheDocument()
    // Collapsed: step rows are NOT rendered; summary line is.
    expect(screen.queryByText("Run tests")).not.toBeInTheDocument()
    expect(screen.getByText(/step 2 in progress/i)).toBeInTheDocument()
    // Click the disclosure to expand.
    await user.click(screen.getByText(/show plan/i))
    expect(screen.getByText("Run tests")).toBeInTheDocument()
  })

  it("shows an error alert when dispatch fails", async () => {
    const dispatch = vi.fn(async () =>
      ({ ok: false, status: 400, body: undefined, error: "unknown tool" }),
    )
    render(
      <CardRunTab
        card={makeCard()}
        question={makeQuestion()}
        run={makeRun()}
        dispatch={dispatch}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: /yes, autofix/i }))
    await waitFor(() =>
      expect(screen.getByRole("alert")).toHaveTextContent(/unknown tool/i),
    )
  })
})
