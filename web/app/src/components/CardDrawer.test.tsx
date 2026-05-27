import { describe, expect, it, vi } from "vitest"
import { fireEvent, render, screen } from "@testing-library/react"
import { CardDrawer } from "./CardDrawer"
import { makeCard, makeQuestion, makeRun } from "../test/fixtures"

const noopDispatch = vi.fn(async () =>
  ({ ok: true, status: 200, body: undefined }),
)

describe("CardDrawer", () => {
  it("renders the card title and ID for the given card", () => {
    render(
      <CardDrawer card={makeCard()} onClose={vi.fn()} dispatch={noopDispatch} />,
    )
    expect(screen.getByText("Wire drawer to backend")).toBeInTheDocument()
    expect(screen.getByText("ABV2-088")).toBeInTheDocument()
  })

  it("calls onClose when ESC is pressed", () => {
    const onClose = vi.fn()
    render(
      <CardDrawer card={makeCard()} onClose={onClose} dispatch={noopDispatch} />,
    )
    fireEvent.keyDown(document, { key: "Escape" })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it("calls onClose when the backdrop is clicked", () => {
    const onClose = vi.fn()
    render(
      <CardDrawer card={makeCard()} onClose={onClose} dispatch={noopDispatch} />,
    )
    fireEvent.click(screen.getByLabelText("Close drawer"))
    expect(onClose).toHaveBeenCalled()
  })

  it("defaults to the Thread tab and switches when History is clicked", () => {
    render(
      <CardDrawer card={makeCard()} onClose={vi.fn()} dispatch={noopDispatch} />,
    )
    const threadTab = screen.getByRole("tab", { name: /thread/i })
    expect(threadTab).toHaveAttribute("aria-selected", "true")
    fireEvent.click(screen.getByRole("tab", { name: /history/i }))
    expect(screen.getByRole("tab", { name: /history/i })).toHaveAttribute(
      "aria-selected",
      "true",
    )
  })

  it("opens on the Run tab when a question or run exists", () => {
    render(
      <CardDrawer
        card={makeCard()}
        question={makeQuestion()}
        run={makeRun()}
        onClose={vi.fn()}
        dispatch={noopDispatch}
      />,
    )
    expect(screen.getByRole("tab", { name: /run/i })).toHaveAttribute(
      "aria-selected",
      "true",
    )
  })
})
