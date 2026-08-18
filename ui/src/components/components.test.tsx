import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { BaselineRowView, SessionOwnedRow } from "./BaselineRow";
import { CommitDialog } from "./CommitDialog";
import { Dial } from "./Dial";
import { ScheduleRowView } from "./ScheduleRow";
import type { BaselineRow, ScheduleRow } from "@/lib/state";

afterEach(cleanup);

const noop = () => {};

/**
 * lib/state.ts was the only tested file in the UI, and the two bugs this project
 * has actually shipped were both render bugs in components with tested pure
 * helpers underneath: the accent dot at zero progress, and the pop-out timer not
 * receiving the current time.
 *
 * These test invariants, never markup.
 */

describe("Dial", () => {
  const text = { countdown: "24:00", status: "locked in" };

  it("is inert once locked", () => {
    // There is no stop verb, so there is no control to render.
    const onTap = vi.fn();
    render(<Dial text={text} fraction={0.5} action="none" neutral={false} onTap={onTap} />);
    fireEvent.click(screen.getByRole("button"));
    expect(onTap).not.toHaveBeenCalled();
    expect(screen.getByRole("button").getAttribute("aria-disabled")).toBe("true");
  });

  it("taps when there is something to do", () => {
    const onTap = vi.fn();
    render(
      <Dial
        text={{ countdown: "50:00", status: "not focusing", hint: "tap to commit" }}
        fraction={0}
        action="commit"
        neutral={false}
        onTap={onTap}
      />,
    );
    fireEvent.click(screen.getByRole("button"));
    expect(onTap).toHaveBeenCalledOnce();
  });

  it("draws no arc at zero, cap included", () => {
    // The shipped bug: a round linecap on a zero-length dash still paints, so
    // IDLE showed an accent dot at twelve o'clock where the spec says empty.
    const { container } = render(
      <Dial text={text} fraction={0} action="commit" neutral={false} onTap={noop} />,
    );
    const arc = container.querySelectorAll("circle")[1];
    expect(arc.getAttribute("stroke-linecap")).toBe("butt");
    expect(arc.getAttribute("stroke-dasharray")?.startsWith("0 ")).toBe(true);
  });

  it("announces itself with the countdown", () => {
    render(<Dial text={text} fraction={0.5} action="none" neutral={false} onTap={noop} />);
    expect(screen.getByRole("button").getAttribute("aria-label")).toContain("24:00");
  });
});

describe("BaselineRowView", () => {
  const row = (over: Partial<BaselineRow> = {}): BaselineRow => ({
    id: "preset.adult",
    enabled: true,
    pendingDisableAt: null,
    ...over,
  });

  // The single most important visual rule on the Blocking screen.
  it("renders a pending disable as ON, because it is still enforced", () => {
    render(
      <BaselineRowView
        row={row({ pendingDisableAt: "2026-08-04T12:15:00Z" })}
        now={Date.parse("2026-08-04T12:00:00Z")}
        last
        busy={false}
        onEnable={noop}
        onDisable={noop}
        onCancel={noop}
      />,
    );
    expect(screen.getByRole("switch").getAttribute("aria-checked")).toBe("true");
    expect(screen.getByText(/unblocks in 15:00/)).toBeTruthy();
  });

  it("sends a settled ON switch to disable, not enable", () => {
    const onDisable = vi.fn();
    const onEnable = vi.fn();
    render(
      <BaselineRowView
        row={row()}
        now={0}
        last
        busy={false}
        onEnable={onEnable}
        onDisable={onDisable}
        onCancel={noop}
      />,
    );
    fireEvent.click(screen.getByRole("switch"));
    expect(onDisable).toHaveBeenCalledOnce();
    expect(onEnable).not.toHaveBeenCalled();
  });

  it("cancels from the pending state rather than starting another disable", () => {
    const onCancel = vi.fn();
    const onDisable = vi.fn();
    render(
      <BaselineRowView
        row={row({ pendingDisableAt: "2026-08-04T12:15:00Z" })}
        now={Date.parse("2026-08-04T12:00:00Z")}
        last
        busy={false}
        onEnable={noop}
        onDisable={onDisable}
        onCancel={onCancel}
      />,
    );
    fireEvent.click(screen.getByRole("switch"));
    expect(onCancel).toHaveBeenCalledOnce();
    expect(onDisable).not.toHaveBeenCalled();
  });
});

describe("SessionOwnedRow", () => {
  it("has no switch — attribution made visible", () => {
    // You can see it is enforced; you do not control it right now.
    render(<SessionOwnedRow id="preset.video" remainingSeconds={2880} last />);
    expect(screen.queryByRole("switch")).toBeNull();
    expect(screen.getByText(/in this session/)).toBeTruthy();
  });
});

describe("ScheduleRowView", () => {
  const row: ScheduleRow = {
    id: "sched.bedtime",
    name: "Bedtime",
    listIds: ["preset.bedtime"],
    start: "23:00",
    end: "07:00",
    tz: "UTC",
    enabled: true,
    active: false,
  };

  it("hides edit and delete while the window is live", () => {
    // The daemon refuses both with would_weaken; a control that cannot do
    // anything is worse than no control.
    render(
      <ScheduleRowView
        row={{ ...row, active: true }}
        last
        busy={false}
        onToggle={noop}
        onEdit={noop}
        onDelete={noop}
      />,
    );
    expect(screen.queryByLabelText(/Edit Bedtime/)).toBeNull();
    expect(screen.queryByLabelText(/Delete Bedtime/)).toBeNull();
    expect(screen.getByText("on now")).toBeTruthy();
  });

  it("offers them when the window is closed", () => {
    render(
      <ScheduleRowView row={row} last busy={false} onToggle={noop} onEdit={noop} onDelete={noop} />,
    );
    expect(screen.getByLabelText(/Edit Bedtime/)).toBeTruthy();
    expect(screen.getByLabelText(/Delete Bedtime/)).toBeTruthy();
  });
});

describe("CommitDialog", () => {
  it("states the terms before they bind", () => {
    render(
      <CommitDialog
        minutes={50}
        blocklistIds={["preset.video"]}
        escapeMinutes={15}
        onCancel={noop}
        onConfirm={noop}
      />,
    );
    expect(screen.getByText(/no off switch/i)).toBeTruthy();
    expect(screen.getByText(/Ending early takes 15 minutes/)).toBeTruthy();
    // Opt-in, and off by default: an open-ended penalty nobody agreed to turns
    // a dead CMOS battery into an unescapable lock.
    expect((screen.getByRole("checkbox") as HTMLInputElement).checked).toBe(false);
  });

  it("is a real dialog, so Escape and the focus trap are the browser's job", () => {
    render(
      <CommitDialog
        minutes={25}
        blocklistIds={[]}
        escapeMinutes={15}
        onCancel={noop}
        onConfirm={noop}
      />,
    );
    expect(screen.getByRole("dialog")).toBeTruthy();
  });

  it("passes the penalty choice up, not a default", () => {
    const onConfirm = vi.fn();
    render(
      <CommitDialog
        minutes={25}
        blocklistIds={[]}
        escapeMinutes={15}
        onCancel={noop}
        onConfirm={onConfirm}
      />,
    );
    fireEvent.click(screen.getByRole("checkbox"));
    fireEvent.click(screen.getByText("Commit"));
    expect(onConfirm).toHaveBeenCalledWith(true);
  });

  it("describes a pomodoro by its shape", () => {
    render(
      <CommitDialog
        minutes={25}
        blocklistIds={["preset.video"]}
        escapeMinutes={15}
        cycles={4}
        breakMinutes={5}
        onCancel={noop}
        onConfirm={noop}
      />,
    );
    expect(screen.getByText(/4 × 25 minutes/)).toBeTruthy();
    // The two things easy to get wrong from outside.
    expect(screen.getByText(/start without a grace window/)).toBeTruthy();
    expect(screen.getByText(/stop for free/)).toBeTruthy();
  });
});
