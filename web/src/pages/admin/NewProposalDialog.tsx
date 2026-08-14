import { useEffect, useState } from "react";
import { proposalApi, userApi } from "../../api/client";
import type { ApiError, DirectoryUser, ProposalType } from "../../api/types";
import ErrorBanner from "../../components/ErrorBanner";
import Spinner from "../../components/Spinner";
import { useModalDialog } from "../../hooks/useModalDialog";
import { personName } from "../../lib/people";
import { runeLength } from "../../lib/post";
import {
  MAX_RATIONALE_LENGTH,
  PROPOSAL_TYPE_OPTIONS,
  eligibleCandidates,
  needsTarget,
  noCandidatesMessage,
  proposalConsequence,
  proposalErrorMessage,
  validateRationale,
} from "../../lib/proposal";

/**
 * How many of the roll to read for the person picker.
 *
 * The server caps the directory at 100 per page and there is no role filter, so
 * this is one page and the filtering happens here. Moderators and council
 * members are a small fraction of a town, and a town large enough for the
 * hundredth-newest member to be a moderator is well past the point where this
 * picker should be a search rather than a list.
 */
const CANDIDATE_PAGE_SIZE = 100;

const fieldStyle: React.CSSProperties = {
  backgroundColor: "var(--color-surface)",
  color: "var(--color-text)",
  borderColor: "var(--color-border-light)",
  borderWidth: "1px",
  borderStyle: "solid",
  borderRadius: "var(--radius-md)",
  padding: "0.5rem 0.75rem",
  width: "100%",
  display: "block",
  outline: "none",
};

/**
 * Raises something for the council to vote on.
 *
 * The consequence of the chosen kind is shown while the proposal is being
 * written, not only once it is on the board: whoever raises it is asking four
 * other people to carry something out immediately, and they should be reading
 * that as they choose the words for why.
 */
export default function NewProposalDialog({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  /** Called after the proposal is on the board, so the page can reload it. */
  onCreated: () => void;
}) {
  const [type, setType] = useState<ProposalType>("council_promotion");
  const [targetId, setTargetId] = useState("");
  const [rationale, setRationale] = useState("");
  const [roll, setRoll] = useState<DirectoryUser[]>([]);
  const [loadingRoll, setLoadingRoll] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const panelRef = useModalDialog<HTMLDivElement>(() => {
    if (!submitting) onClose();
  });

  // Read once when the dialog opens rather than with the page: the directory is
  // only needed by whoever is actually raising a proposal, which is the rarer
  // half of visiting the town hall.
  useEffect(() => {
    let cancelled = false;

    userApi
      .list(CANDIDATE_PAGE_SIZE, 0)
      .then((res) => {
        if (!cancelled) setRoll(res.users ?? []);
      })
      .catch(() => {
        // Left as an empty roll. The picker says there is nobody to choose,
        // which is also what an empty page of results looks like — and either
        // way the honest next step is the same: this proposal cannot be raised
        // from here right now.
        if (!cancelled) setRoll([]);
      })
      .finally(() => {
        if (!cancelled) setLoadingRoll(false);
      });

    return () => {
      cancelled = true;
    };
  }, []);

  const candidates = eligibleCandidates(roll, type);
  const check = validateRationale(rationale);
  const chars = runeLength(rationale.trim());
  const targetRequired = needsTarget(type);
  const canSubmit = check.valid && !submitting && (!targetRequired || targetId !== "");

  function chooseType(next: ProposalType) {
    setType(next);
    // The chosen person belongs to the old kind's list of candidates; carrying
    // them across would mean proposing to remove somebody picked from a list of
    // moderators.
    setTargetId("");
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;

    setSubmitting(true);
    setError(null);

    try {
      await proposalApi.create({
        type,
        ...(targetRequired ? { target_user_id: targetId } : {}),
        rationale: rationale.trim(),
      });
      onCreated();
      onClose();
    } catch (err) {
      setError(proposalErrorMessage(err as ApiError, "create"));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/50 p-4"
      role="dialog"
      aria-modal="true"
      aria-label="Raise a proposal"
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
        <h2 className="mb-2 text-lg font-semibold" style={{ color: "var(--color-text)" }}>
          Raise a proposal
        </h2>
        <p
          className="mb-4 text-sm leading-relaxed"
          style={{ color: "var(--color-text-secondary)" }}
        >
          The whole council votes on this, and a majority carries it.
        </p>

        <form onSubmit={handleSubmit} className="flex flex-col gap-4">
          <div>
            <label
              htmlFor="proposal-type"
              className="mb-1 block text-sm font-medium"
              style={{ color: "var(--color-text-secondary)" }}
            >
              What is being proposed?
            </label>
            <select
              id="proposal-type"
              value={type}
              onChange={(e) => chooseType(e.target.value as ProposalType)}
              disabled={submitting}
              style={fieldStyle}
            >
              {PROPOSAL_TYPE_OPTIONS.map((option) => (
                <option key={option.value} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
            <p className="mt-1 text-xs leading-relaxed" style={{ color: "var(--color-text-tertiary)" }}>
              {proposalConsequence(type)}
            </p>
          </div>

          {targetRequired && (
            <div>
              <label
                htmlFor="proposal-target"
                className="mb-1 block text-sm font-medium"
                style={{ color: "var(--color-text-secondary)" }}
              >
                Who?
              </label>
              {loadingRoll ? (
                <div className="py-2">
                  <Spinner size="sm" />
                </div>
              ) : candidates.length === 0 ? (
                <p className="text-sm" style={{ color: "var(--color-text-tertiary)" }}>
                  {noCandidatesMessage(type)}
                </p>
              ) : (
                <select
                  id="proposal-target"
                  value={targetId}
                  onChange={(e) => setTargetId(e.target.value)}
                  disabled={submitting}
                  style={fieldStyle}
                >
                  <option value="">Choose someone</option>
                  {candidates.map((candidate) => (
                    <option key={candidate.id} value={candidate.id}>
                      {personName(candidate.display_name, candidate.id)}
                    </option>
                  ))}
                </select>
              )}
            </div>
          )}

          <div>
            <label
              htmlFor="proposal-rationale"
              className="mb-1 block text-sm font-medium"
              style={{ color: "var(--color-text-secondary)" }}
            >
              Why?
            </label>
            <textarea
              id="proposal-rationale"
              value={rationale}
              onChange={(e) => setRationale(e.target.value)}
              rows={4}
              disabled={submitting}
              placeholder="What should the rest of the council know before they vote?"
              style={{ ...fieldStyle, resize: "none" }}
            />
            {/*
              Counted in characters, because the contract bounds characters — and
              only once the limit is in sight, on the same reasoning as the report
              dialog's counter.
            */}
            {chars > MAX_RATIONALE_LENGTH / 2 && (
              <p className="mt-1 text-xs" style={{ color: "var(--color-text-tertiary)" }}>
                {chars}/{MAX_RATIONALE_LENGTH} characters
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
              disabled={!canSubmit}
              className="rounded-md px-4 py-2 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
              style={{
                backgroundColor: "var(--color-primary)",
                color: "var(--color-text-inverse)",
              }}
            >
              {submitting ? (
                <span className="inline-flex items-center gap-2">
                  <Spinner size="sm" />
                  Raising...
                </span>
              ) : (
                "Put it to the council"
              )}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
