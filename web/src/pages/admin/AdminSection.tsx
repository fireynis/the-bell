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
  children,
}: {
  title: string;
  isEmpty: boolean;
  emptyMessage: string;
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
      <h2
        className={isEmpty ? "text-lg font-semibold" : "mb-4 text-lg font-semibold"}
        style={{ color: "var(--color-text)" }}
      >
        {title}
      </h2>
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
