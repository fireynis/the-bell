import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import PostMenu, { type PostMenuItem } from "./PostMenu";

/**
 * The overflow menu is the only way to reach reporting, editing and deleting, so
 * these pin the parts that make it usable without a mouse: it must open, be
 * escapable, and hand focus back where it came from.
 */

function item(overrides: Partial<PostMenuItem> = {}): PostMenuItem {
  return { label: "Report post", onSelect: () => {}, ...overrides };
}

function open(label = "Actions for Ada's post") {
  const trigger = screen.getByRole("button", { name: label });
  fireEvent.click(trigger);
  return trigger;
}

describe("PostMenu", () => {
  it("renders nothing when there is nothing to offer", () => {
    const { container } = render(<PostMenu items={[]} label="Actions for Ada's post" />);
    expect(container).toBeEmptyDOMElement();
  });

  it("names the trigger after whose post it acts on", () => {
    render(<PostMenu items={[item()]} label="Actions for Ada's post" />);
    expect(screen.getByRole("button", { name: "Actions for Ada's post" })).toBeTruthy();
  });

  it("keeps the menu closed until asked", () => {
    render(<PostMenu items={[item()]} label="Actions for Ada's post" />);
    expect(screen.getByRole("button", { name: /Actions for/ }).getAttribute("aria-expanded")).toBe(
      "false",
    );
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("opens on click and says so", () => {
    render(<PostMenu items={[item()]} label="Actions for Ada's post" />);
    const trigger = open();
    expect(trigger.getAttribute("aria-expanded")).toBe("true");
    expect(screen.getByRole("menuitem", { name: "Report post" })).toBeTruthy();
  });

  it("focuses the first item on open, so the keyboard lands inside the menu", () => {
    render(<PostMenu items={[item()]} label="Actions for Ada's post" />);
    open();
    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Report post" }));
  });

  // The edit window can close while a card sits on screen, so the caller gets a
  // chance to recompute the items before they are read.
  it("tells the caller it is opening, and only when opening", () => {
    const onOpen = vi.fn();
    render(<PostMenu items={[item()]} label="Actions for Ada's post" onOpen={onOpen} />);

    const trigger = open();
    expect(onOpen).toHaveBeenCalledTimes(1);

    fireEvent.click(trigger);
    expect(onOpen).toHaveBeenCalledTimes(1);
  });

  it("runs the selected action and closes", () => {
    const onSelect = vi.fn();
    render(<PostMenu items={[item({ onSelect })]} label="Actions for Ada's post" />);
    open();

    fireEvent.click(screen.getByRole("menuitem", { name: "Report post" }));

    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("closes on Escape and hands focus back to the trigger", () => {
    render(<PostMenu items={[item()]} label="Actions for Ada's post" />);
    const trigger = open();

    fireEvent.keyDown(screen.getByRole("menu"), { key: "Escape" });

    expect(screen.queryByRole("menu")).toBeNull();
    expect(document.activeElement).toBe(trigger);
  });

  it("closes when the reader clicks elsewhere on the page", () => {
    render(<PostMenu items={[item()]} label="Actions for Ada's post" />);
    open();

    fireEvent.pointerDown(document.body);

    expect(screen.queryByRole("menu")).toBeNull();
  });

  it("moves between items with the arrow keys", () => {
    render(
      <PostMenu
        items={[item({ label: "Edit post" }), item({ label: "Delete post" })]}
        label="Actions for Ada's post"
      />,
    );
    open();

    fireEvent.keyDown(screen.getByRole("menu"), { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Delete post" }));

    // Wraps, so a keyboard user is never stuck at either end of a short menu.
    fireEvent.keyDown(screen.getByRole("menu"), { key: "ArrowDown" });
    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Edit post" }));
  });

  // A post already reported keeps its item, so the menu answers "did that go
  // through?" — but selecting it again would be a guaranteed 400.
  it("skips a disabled item when opening and does not run it", () => {
    const onSelect = vi.fn();
    render(
      <PostMenu
        items={[item({ label: "Reported", disabled: true, onSelect }), item({ label: "Edit post" })]}
        label="Actions for Ada's post"
      />,
    );
    open();

    expect(document.activeElement).toBe(screen.getByRole("menuitem", { name: "Edit post" }));

    fireEvent.click(screen.getByRole("menuitem", { name: "Reported" }));
    expect(onSelect).not.toHaveBeenCalled();
  });
});
