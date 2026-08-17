import { useRef, useState } from "react";
import { inviteApi } from "../../api/client";
import type { ApiError, CreateInviteResponse } from "../../api/types";
import ErrorBanner from "../../components/ErrorBanner";
import Spinner from "../../components/Spinner";
import { useModalDialog } from "../../hooks/useModalDialog";
import {
  INVITE_CONSEQUENCE,
  INVITE_DIALOG,
  MAX_INVITE_NOTE_LENGTH,
  inviteErrorMessage,
  remainingNoteChars,
  validateInviteEmail,
  validateInviteNote,
} from "../../lib/invite";

const fieldStyle: React.CSSProperties = {
  borderColor: "var(--color-border)",
  borderWidth: "1px",
  borderStyle: "solid",
  borderRadius: "var(--radius-md)",
  padding: "0.5rem 0.75rem",
  width: "100%",
  display: "block",
  outline: "none",
  backgroundColor: "var(--color-surface)",
  color: "var(--color-text)",
};

interface InviteDialogProps {
  onClose: () => void;
  /** Called once the invitation exists, so the sender's list can pick it up. */
  onInvited: () => void;
}

/** Whether the link has been put on the clipboard, or merely selected for the reader to copy. */
type CopyState = "idle" | "copied" | "selected";

/**
 * InviteDialog sends an invitation, then hands back the link it created.
 *
 * The dialog does not close on success, and that is the whole design. The raw
 * token is in the response and nowhere else — it is never listed, never sent
 * again — so a dialog that vanished on 201 would destroy the only copy of the
 * link in any case where the email did not arrive. What replaces the form is
 * the link itself, with a way to copy it and a plain statement of whether the
 * town managed to send it.
 *
 * The consequence line is above the button rather than in a confirm dialog
 * behind it. Inviting somebody spends a vouch and stakes standing on them, and
 * a person deciding whether to type an address should read that while they are
 * deciding, not after they have decided.
 */
export default function InviteDialog({ onClose, onInvited }: InviteDialogProps) {
  const [email, setEmail] = useState("");
  const [note, setNote] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<CreateInviteResponse | null>(null);
  const [copyState, setCopyState] = useState<CopyState>("idle");
  const linkRef = useRef<HTMLInputElement>(null);

  const panelRef = useModalDialog<HTMLDivElement>(() => {
    if (!submitting) onClose();
  });

  const emailCheck = validateInviteEmail(email);
  const noteCheck = validateInviteNote(note);
  const remaining = remainingNoteChars(note);

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (submitting) return;

    const failure = !emailCheck.valid ? emailCheck : !noteCheck.valid ? noteCheck : null;
    if (failure) {
      setError(failure.error ?? null);
      return;
    }

    setSubmitting(true);
    setError(null);
    try {
      const created = await inviteApi.create({ email, note });
      setResult(created);
      onInvited();
    } catch (err) {
      setError(inviteErrorMessage(err as ApiError));
    } finally {
      setSubmitting(false);
    }
  }

  /**
   * Puts the link on the clipboard, falling back to selecting it.
   *
   * navigator.clipboard is absent outside a secure context and in older
   * embedded browsers, and `await undefined` resolves — so its presence is
   * checked rather than optional-chained, or a reader on http would be told the
   * link was copied when nothing was. Selecting the text leaves them one
   * keystroke away instead of stranded.
   */
  async function handleCopy() {
    const url = result?.invite_url ?? "";
    try {
      const clipboard = navigator.clipboard;
      if (!clipboard?.writeText) throw new Error("clipboard unavailable");
      await clipboard.writeText(url);
      setCopyState("copied");
    } catch {
      linkRef.current?.select();
      setCopyState("selected");
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-label={INVITE_DIALOG.title}
    >
      <div
        ref={panelRef}
        className="w-full max-w-md p-6"
        style={{
          backgroundColor: "var(--color-surface)",
          boxShadow: "var(--shadow-lg)",
          borderRadius: "var(--radius-lg)",
        }}
      >
        {result ? (
          <>
            <h2 className="mb-2 text-lg font-semibold" style={{ color: "var(--color-text)" }}>
              {INVITE_DIALOG.readyTitle}
            </h2>
            <p
              className="mb-4 text-sm leading-relaxed"
              style={{ color: "var(--color-text-secondary)" }}
              role="status"
            >
              {result.email_sent ? INVITE_DIALOG.emailSent : INVITE_DIALOG.emailFailed}
            </p>

            {/*
              Quietly, and under the sentence it explains. The sender cannot act
              on an SMTP error, but leaving it out entirely turns "the email
              couldn't be sent" into something they have to take on faith.
            */}
            {!result.email_sent && result.email_error && (
              <p className="mb-4 text-xs" style={{ color: "var(--color-text-tertiary)" }}>
                {result.email_error}
              </p>
            )}

            <label
              htmlFor="invite-link"
              className="mb-1 block text-sm font-medium"
              style={{ color: "var(--color-text-secondary)" }}
            >
              {INVITE_DIALOG.linkLabel}
            </label>
            <div className="flex gap-2">
              <input
                id="invite-link"
                ref={linkRef}
                readOnly
                value={result.invite_url}
                onFocus={(e) => e.currentTarget.select()}
                className="text-sm"
                style={{ ...fieldStyle, flex: 1 }}
              />
              <button
                type="button"
                onClick={handleCopy}
                className="flex-shrink-0 rounded-md px-3 py-2 text-sm font-medium"
                style={{
                  backgroundColor: "var(--color-surface-tertiary)",
                  color: "var(--color-text-secondary)",
                }}
              >
                {copyState === "copied" ? INVITE_DIALOG.copied : INVITE_DIALOG.copy}
              </button>
            </div>
            {/*
              One live region for both outcomes, so a screen reader hears which
              of the two happened rather than watching a button's label change.
            */}
            <p
              className="mt-1 min-h-4 text-xs"
              style={{ color: "var(--color-text-tertiary)" }}
              role="status"
            >
              {copyState === "copied"
                ? INVITE_DIALOG.copiedNotice
                : copyState === "selected"
                  ? INVITE_DIALOG.selected
                  : ""}
            </p>

            <div className="mt-5 flex justify-end">
              <button
                type="button"
                onClick={onClose}
                className="rounded-md px-4 py-2 text-sm font-medium"
                style={{
                  backgroundColor: "var(--color-primary)",
                  color: "var(--color-text-inverse)",
                }}
              >
                {INVITE_DIALOG.done}
              </button>
            </div>
          </>
        ) : (
          <>
            <h2 className="mb-2 text-lg font-semibold" style={{ color: "var(--color-text)" }}>
              {INVITE_DIALOG.title}
            </h2>
            <p
              className="mb-4 text-sm leading-relaxed"
              style={{ color: "var(--color-text-secondary)" }}
            >
              {INVITE_CONSEQUENCE}
            </p>

            <form onSubmit={handleSubmit} className="flex flex-col gap-4">
              <div>
                <label
                  htmlFor="invite-email"
                  className="mb-1 block text-sm font-medium"
                  style={{ color: "var(--color-text-secondary)" }}
                >
                  {INVITE_DIALOG.emailLabel}
                </label>
                <input
                  id="invite-email"
                  type="email"
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  disabled={submitting}
                  autoComplete="off"
                  placeholder={INVITE_DIALOG.emailPlaceholder}
                  className="text-sm"
                  style={fieldStyle}
                />
              </div>

              <div>
                <label
                  htmlFor="invite-note"
                  className="mb-1 block text-sm font-medium"
                  style={{ color: "var(--color-text-secondary)" }}
                >
                  {INVITE_DIALOG.noteLabel}
                </label>
                <textarea
                  id="invite-note"
                  value={note}
                  onChange={(e) => setNote(e.target.value)}
                  rows={3}
                  disabled={submitting}
                  placeholder={INVITE_DIALOG.notePlaceholder}
                  className="text-sm"
                  style={{ ...fieldStyle, resize: "none" }}
                />
                <p className="mt-1 text-xs" style={{ color: "var(--color-text-tertiary)" }}>
                  {INVITE_DIALOG.noteHelp}
                </p>
                {/*
                  Only once the limit is in sight. The number is the server's
                  rule rather than a target, and counting down from 500 beside a
                  one-line note would read as a demand for more.
                */}
                {remaining <= MAX_INVITE_NOTE_LENGTH / 2 && (
                  <p
                    className="mt-1 text-xs"
                    style={{
                      color: remaining < 0 ? "var(--color-danger)" : "var(--color-text-tertiary)",
                    }}
                  >
                    {remaining} characters left
                  </p>
                )}
              </div>

              {error && <ErrorBanner message={error} />}

              <div className="flex justify-end gap-3">
                <button
                  type="button"
                  onClick={onClose}
                  disabled={submitting}
                  className="rounded-md px-4 py-2 text-sm font-medium disabled:opacity-50"
                  style={{
                    backgroundColor: "var(--color-surface-tertiary)",
                    color: "var(--color-text-secondary)",
                  }}
                >
                  Cancel
                </button>
                <button
                  type="submit"
                  disabled={submitting || !emailCheck.valid || !noteCheck.valid}
                  className="rounded-md px-4 py-2 text-sm font-medium disabled:opacity-50"
                  style={{
                    backgroundColor: "var(--color-primary)",
                    color: "var(--color-text-inverse)",
                  }}
                >
                  {submitting ? (
                    <span className="inline-flex items-center gap-2">
                      <Spinner size="sm" />
                      {INVITE_DIALOG.sending}
                    </span>
                  ) : (
                    INVITE_DIALOG.submit
                  )}
                </button>
              </div>
            </form>
          </>
        )}
      </div>
    </div>
  );
}
