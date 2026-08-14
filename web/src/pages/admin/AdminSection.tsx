import type { ReactNode } from "react";

/**
 * AdminSection is the card every admin panel sits in: the same surface, the
 * same heading, and the same "nothing here yet" line.
 *
 * Each section previously carried its own copy of this markup, duplicated once
 * more inside itself for the empty case — four near-identical blocks whose
 * padding and radius had to be kept in step by hand. Passing `isEmpty` rather
 * than letting callers branch keeps the empty state looking like the populated
 * one by construction.
 */
export default function AdminSection({
  title,
  isEmpty,
  emptyMessage,
  action,
  children,
}: {
  title: string;
  isEmpty: boolean;
  emptyMessage: string;
  /**
   * A control belonging to the section as a whole, shown beside its heading.
   *
   * It sits outside `children` because it has to survive the empty state: the
   * moment a town hall has nothing open is exactly the moment somebody wants to
   * raise something, and a button that disappears when the list is empty is a
   * dead end.
   */
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <div
      className="p-6"
      style={{
        backgroundColor: "var(--color-surface)",
        boxShadow: "var(--shadow-sm)",
        borderRadius: "var(--radius-lg)",
      }}
    >
      <div
        className={`flex flex-wrap items-center justify-between gap-3${isEmpty ? "" : " mb-4"}`}
      >
        <h2 className="text-lg font-semibold" style={{ color: "var(--color-text)" }}>
          {title}
        </h2>
        {action}
      </div>
      {isEmpty ? (
        <p className="mt-2 text-sm" style={{ color: "var(--color-text-secondary)" }}>
          {emptyMessage}
        </p>
      ) : (
        children
      )}
    </div>
  );
}
