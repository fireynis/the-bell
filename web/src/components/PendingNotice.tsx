import { Link } from "react-router";
import { PENDING_WELCOME } from "../lib/gating";
import ResidencyClaimField from "./ResidencyClaimField";

interface PendingNoticeProps {
  /** Extra spacing for the page it sits on; the notice owns nothing else. */
  className?: string;
}

/**
 * PendingNotice greets a member the town has not rung in yet.
 *
 * A new arrival used to be dropped on the feed with no explanation at all: the
 * word "pending" appeared nowhere, the composer looked available, and the first
 * thing anyone learned about the rule was a disabled textarea. This says the
 * same three things the composer's gate line says, at the length it takes to
 * not read as a rejection — what pending means, what ends it, and what there is
 * to do in the meantime.
 *
 * Home and Compose render the same component so the two cannot drift into
 * telling different stories, which is also why the strings live in lib/gating
 * beside the one-line version of them.
 */
export default function PendingNotice({ className }: PendingNoticeProps) {
  return (
    <section
      className={`rounded-[var(--radius-lg)] p-4${className ? ` ${className}` : ""}`}
      style={{
        backgroundColor: "var(--color-primary-subtle)",
        border: "1px solid var(--color-primary-light)",
      }}
      role="status"
      aria-label={PENDING_WELCOME.title}
      data-testid="pending-notice"
    >
      <h2
        className="text-base font-semibold"
        style={{ fontFamily: "var(--font-display)", color: "var(--color-text)" }}
      >
        {PENDING_WELCOME.title}
      </h2>

      <p className="mt-2 text-sm leading-relaxed" style={{ color: "var(--color-text-secondary)" }}>
        {PENDING_WELCOME.meaning}
      </p>
      <p className="mt-2 text-sm leading-relaxed" style={{ color: "var(--color-text-secondary)" }}>
        {PENDING_WELCOME.next}
      </p>
      <p className="mt-2 text-sm leading-relaxed" style={{ color: "var(--color-text-secondary)" }}>
        {PENDING_WELCOME.meanwhile}
      </p>

      <Link
        to="/profile"
        className="mt-3 inline-block text-sm font-semibold"
        style={{ color: "var(--color-primary)" }}
      >
        {PENDING_WELCOME.profileCta}
      </Link>

      {/*
        The other half of "someone has to recognise you": the profile link sends
        them off to fill something in, this asks for the one thing the council
        actually reads while deciding. It is inside the welcome rather than after
        it because it only makes sense as an answer to what the welcome just
        said.
      */}
      <ResidencyClaimField />
    </section>
  );
}
