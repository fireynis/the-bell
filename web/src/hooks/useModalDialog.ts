import { useEffect, useRef } from "react";

/**
 * What counts as reachable by keyboard inside a dialog. Deliberately narrow:
 * anything with a negative tabindex is reachable by script only, and including
 * it would put a stop in the Tab cycle that a mouse user never sees.
 */
const FOCUSABLE_SELECTOR = [
  "a[href]",
  "button:not([disabled])",
  "textarea:not([disabled])",
  "input:not([disabled])",
  "select:not([disabled])",
  '[tabindex]:not([tabindex="-1"])',
].join(", ");

/**
 * useModalDialog gives an `aria-modal` dialog the three behaviours the attribute
 * promises: focus starts inside it, Tab cannot leave it, and Escape closes it.
 *
 * Attach the returned ref to the dialog's panel — the element holding the
 * controls, not the backdrop, so a click on the backdrop is not treated as being
 * inside the trap.
 *
 * `onClose` is read through a ref and the effect runs exactly once. Depending on
 * it directly would re-run the effect on every render of a caller that passes an
 * inline arrow function, and re-running means moving focus back to the first
 * control — which, in a dialog whose first control is a textarea, throws the
 * caret to the start of the field on every keystroke.
 *
 * Focus returns to whatever opened the dialog on unmount. That element is often
 * a menu item that has itself gone away by then; focusing a detached node is a
 * no-op, so the browser falls back to the body rather than erroring.
 */
export function useModalDialog<T extends HTMLElement>(onClose: () => void) {
  const panelRef = useRef<T>(null);
  const closeRef = useRef(onClose);

  useEffect(() => {
    closeRef.current = onClose;
  });

  useEffect(() => {
    const opener = document.activeElement as HTMLElement | null;
    const panel = panelRef.current;

    const focusable = () =>
      Array.from(panel?.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR) ?? []);

    focusable()[0]?.focus();

    const handleKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        // Stopped so one Escape does not also close a dialog this one was
        // opened from, or the menu underneath it.
        e.stopPropagation();
        closeRef.current();
        return;
      }
      if (e.key !== "Tab") return;

      const items = focusable();
      if (items.length === 0) return;

      const first = items[0];
      const last = items[items.length - 1];
      const active = document.activeElement;

      // Focus outside the panel is treated as being before the first item, so a
      // Shift+Tab that started nowhere in particular lands on the last control
      // rather than escaping into the page behind.
      if (e.shiftKey && (active === first || !panel?.contains(active))) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && active === last) {
        e.preventDefault();
        first.focus();
      }
    };

    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("keydown", handleKeyDown);
      opener?.focus?.();
    };
  }, []);

  return panelRef;
}
