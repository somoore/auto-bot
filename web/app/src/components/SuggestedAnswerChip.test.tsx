import { describe, expect, it, vi } from "vitest"
import { fireEvent, render, screen } from "@testing-library/react"
import { SuggestedAnswerChip } from "./SuggestedAnswerChip"

describe("SuggestedAnswerChip", () => {
  it("renders the label and index", () => {
    render(<SuggestedAnswerChip index={2} label="Yes, autofix" onClick={vi.fn()} />)
    expect(screen.getByText("Yes, autofix")).toBeInTheDocument()
    expect(screen.getByText("2")).toBeInTheDocument()
  })

  it("shows the recommended marker when recommended is true", () => {
    render(
      <SuggestedAnswerChip
        index={1}
        label="Yes, autofix"
        recommended
        onClick={vi.fn()}
      />,
    )
    expect(screen.getByText(/recommended/i)).toBeInTheDocument()
  })

  it("does not show the recommended marker otherwise", () => {
    render(
      <SuggestedAnswerChip index={3} label="Show diff" onClick={vi.fn()} />,
    )
    expect(screen.queryByText(/recommended/i)).not.toBeInTheDocument()
  })

  it("fires the click handler", () => {
    const onClick = vi.fn()
    render(
      <SuggestedAnswerChip index={1} label="Yes" onClick={onClick} />,
    )
    fireEvent.click(screen.getByRole("button"))
    expect(onClick).toHaveBeenCalledTimes(1)
  })

  it("is disabled when disabled prop is set", () => {
    const onClick = vi.fn()
    render(
      <SuggestedAnswerChip
        index={1}
        label="Yes"
        disabled
        onClick={onClick}
      />,
    )
    const btn = screen.getByRole("button")
    expect(btn).toBeDisabled()
    fireEvent.click(btn)
    expect(onClick).not.toHaveBeenCalled()
  })
})
