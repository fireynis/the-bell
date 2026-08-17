import { useEffect, useRef, useState } from "react";

export interface PostMenuItem {
  label: string;
  onSelect: () => void;
  /** Styles the item as destructive, for taking something down. */
  danger?: boolean;
  /** Kept visible but unselectable, e.g. a post already reported. */
  disabled?: boolean;
}

interface PostMenuProps {
  items: PostMenuItem[];
  /** Accessible name for the trigger; say whose post it opens. */
  label: string;
  /**
   * Called as the menu opens, before the items are read.
   *
   * Which items belong here can go stale while a card sits on screen — the
   * fifteen-minute edit window closes without anything re-rendering the feed —
   * so this is the caller's chance to recompute against the current time.
   */
  onOpen?: () => void;
}

/**
 * PostMenu is the quiet overflow control on a post card.
 *
 * It is deliberately low-contrast and unlabelled until hovered or focused. The
 * things behind it — reporting a neighbour, deleting what you said — are not
 * what this town square is for, and a prominent button invites the click on its
 * own. The reactions beside it are the loud controls; this one is meant to be
 * found only by someone already looking for it.
 *
 * Renders nothing when there is nothing to offer, so a card with no available
 * actions carries no empty affordance.
 */
export default function PostMenu({ items, label, onOpen }: PostMenuProps) {
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;

    menuRef.current?.querySelector<HTMLButtonElement>("button:not([disabled])")?.focus();

    // pointerdown rather than click: a click listener fires after the mousedown
    // has already moved focus, so a menu closed on click would fight the focus
    // restore below for whatever the user was reaching for.
    const handlePointerDown = (e: PointerEvent) => {
      if (!containerRef.current?.contains(e.target as Node)) setOpen(false);
    };

    document.addEventListener("pointerdown", handlePointerDown);
    return () => document.removeEventListener("pointerdown", handlePointerDown);
  }, [open]);

  function closeAndRefocus() {
    setOpen(false);
    triggerRef.current?.focus();
  }

  function handleKeyDown(e: React.KeyboardEvent) {
    if (e.key === "Escape") {
      e.stopPropagation();
      closeAndRefocus();
      return;
    }
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp") return;

    const buttons = Array.from(
      menuRef.current?.querySelectorAll<HTMLButtonElement>("button:not([disabled])") ?? [],
    );
    if (buttons.length === 0) return;

    e.preventDefault();
    const current = buttons.indexOf(document.activeElement as HTMLButtonElement);
    if (current === -1) {
      buttons[0].focus();
      return;
    }
    const step = e.key === "ArrowDown" ? 1 : -1;
    buttons[(current + step + buttons.length) % buttons.length].focus();
  }

  function select(item: PostMenuItem) {
    // Focus moves to the trigger before the item runs, while the menu is still
    // mounted. Every item here opens a dialog, and a dialog restores focus to
    // whatever held it when the dialog opened — which would otherwise be the
    // menu item that this very click is about to unmount, leaving focus on the
    // document body when the dialog closes.
    setOpen(false);
    triggerRef.current?.focus();
    item.onSelect();
  }

  if (items.length === 0) return null;

  return (
    <div ref={containerRef} className="relative" onKeyDown={handleKeyDown}>
      <button
        ref={triggerRef}
        type="button"
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => {
          if (open) {
            closeAndRefocus();
            return;
          }
          onOpen?.();
          setOpen(true);
        }}
        className="tint-on-hover flex h-7 w-7 items-center justify-center rounded-full text-base leading-none"
        style={{ color: "var(--color-text-tertiary)" }}
      >
        <span aria-hidden="true">&hellip;</span>
      </button>

      {open && (
        <div
          ref={menuRef}
          role="menu"
          aria-label={label}
          className="absolute right-0 z-20 mt-1 min-w-40 overflow-hidden py-1"
          style={{
            backgroundColor: "var(--color-surface)",
            boxShadow: "var(--shadow-lg)",
            borderRadius: "var(--radius-md)",
            border: "1px solid var(--color-border-light)",
          }}
        >
          {items.map((item) => (
            <button
              key={item.label}
              type="button"
              role="menuitem"
              disabled={item.disabled}
              onClick={() => select(item)}
              className="tint-on-hover block w-full px-4 py-2 text-left text-sm disabled:opacity-60"
              style={{ color: item.danger ? "var(--color-danger)" : "var(--color-text-secondary)" }}
            >
              {item.label}
            </button>
          ))}
        </div>
      )}
    </div>
  );
}
