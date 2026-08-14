import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import MuteToggle from "./MuteToggle";

/**
 * The bell emoji carries the state visually, and it is aria-hidden. These pin
 * the half a screen reader gets: without an accessible name and a pressed state,
 * the control is an unlabelled button that silently does something.
 */

describe("MuteToggle", () => {
  it("offers to mute while sounds are on", () => {
    render(<MuteToggle muted={false} arrivals={0} onToggle={() => {}} />);
    const button = screen.getByRole("button", { name: "Mute bell sounds" });
    expect(button.getAttribute("aria-pressed")).toBe("false");
  });

  it("offers to unmute once they are off", () => {
    render(<MuteToggle muted arrivals={0} onToggle={() => {}} />);
    const button = screen.getByRole("button", { name: "Unmute bell sounds" });
    expect(button.getAttribute("aria-pressed")).toBe("true");
  });

  it("reports the toggle to its owner, which is where the state lives", () => {
    const onToggle = vi.fn();
    render(<MuteToggle muted={false} arrivals={0} onToggle={onToggle} />);

    fireEvent.click(screen.getByRole("button", { name: "Mute bell sounds" }));

    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  // The animation is decoration on a control whose meaning is already spoken.
  it("animates the bell on an arrival without announcing the emoji", () => {
    render(<MuteToggle muted={false} arrivals={3} onToggle={() => {}} />);
    const emoji = screen.getByRole("button", { name: "Mute bell sounds" }).querySelector("span");
    expect(emoji?.getAttribute("aria-hidden")).toBe("true");
    expect(emoji?.className).toContain("animate-ring");
  });
});
