import { useState } from "react";
import { userApi } from "../api/client.ts";
import type { ApiError } from "../api/types.ts";
import { useAuth } from "../context/AuthContext.tsx";
import { RESIDENCY_PROMPT } from "../lib/gating.ts";
import { runeLength } from "../lib/post.ts";
import {
  MAX_RESIDENCY_CLAIM_LENGTH,
  residencyClaimErrorMessage,
  residencyClaimOf,
  validateResidencyClaim,
} from "../lib/residency.ts";
import ErrorBanner from "./ErrorBanner.tsx";

/**
 * Lets a member the town has not rung in yet say where in town they are.
 *
 * It sits inside the welcome rather than on the profile form on purpose. This is
 * the one moment the claim is useful — a council member is about to decide
 * whether they recognise a stranger — and the one moment the member has a reason
 * to answer. On a profile page, beside a bio and an avatar, the same box would
 * read as another public field to fill in, which is exactly what it is not.
 *
 * Answering is optional and saying so is part of the design: nothing here gates
 * approval, and the field is never marked required.
 */
export default function ResidencyClaimField() {
  const { user, updateUser } = useAuth();

  const profileClaim = residencyClaimOf(user);

  // Seeded from the profile if this build's server sends the claim back on the
  // self view, and from nothing if it does not.
  const [claim, setClaim] = useState(profileClaim);
  const [seed, setSeed] = useState(profileClaim);
  const [touched, setTouched] = useState(false);
  const [saved, setSaved] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [focused, setFocused] = useState(false);

  // The profile is fetched, so it can arrive after this has already rendered —
  // an initialiser alone would leave a member who has answered before staring at
  // an empty box. Adjusted during render rather than in an effect so the field is
  // never painted blank and then filled in.
  //
  // Only until the member touches it: once they are typing, a profile refresh
  // arriving mid-sentence must not reach in and overwrite what they are writing.
  if (!touched && profileClaim !== seed) {
    setSeed(profileClaim);
    setClaim(profileClaim);
  }

  const check = validateResidencyClaim(claim);
  const chars = runeLength(claim.trim());
  // Unchanged means there is nothing to send: the server would accept it and
  // store the same string, and the button would be inviting a member to keep
  // confirming something they have already said.
  const unchanged = claim.trim() === (saved ?? profileClaim);
  const canSave = check.valid && !saving && !unchanged;

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSave) return;

    const value = claim.trim();
    setSaving(true);
    setError(null);

    try {
      await userApi.setResidencyClaim(value);
      setSaved(value);
      // The cached profile is updated too, so moving between the feed and the
      // composer — both of which render this welcome — shows what was saved
      // rather than an empty box. The server has already accepted the string,
      // so this is not a guess about what it stored.
      if (user) updateUser({ ...user, residency_claim: value });
    } catch (err) {
      // The typed text is deliberately left alone. It is the member's own words
      // about where they live, and clearing it on a failed request would make
      // them type it again to find out whether the second attempt works.
      setError(residencyClaimErrorMessage(err as ApiError));
    } finally {
      setSaving(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="mt-4">
      <label
        htmlFor="residency-claim"
        className="block text-sm font-semibold"
        style={{ color: "var(--color-text)" }}
      >
        {RESIDENCY_PROMPT.label}
      </label>
      <p
        id="residency-claim-help"
        className="mt-1 text-sm leading-relaxed"
        style={{ color: "var(--color-text-secondary)" }}
      >
        {RESIDENCY_PROMPT.help}
      </p>

      <div className="mt-2 flex flex-wrap items-start gap-2">
        <input
          id="residency-claim"
          type="text"
          value={claim}
          onChange={(e) => {
            setClaim(e.target.value);
            setTouched(true);
            // The acknowledgement belongs to the string that was saved, not to
            // the field, so it goes the moment the field says something else.
            setSaved((current) => (current === e.target.value.trim() ? current : null));
          }}
          onFocus={() => setFocused(true)}
          onBlur={() => setFocused(false)}
          disabled={saving}
          autoComplete="off"
          placeholder={RESIDENCY_PROMPT.placeholder}
          aria-describedby="residency-claim-help"
          className="min-w-0 flex-1 rounded-[var(--radius-md)] border px-3 py-2 text-sm"
          style={{
            backgroundColor: "var(--color-surface)",
            color: "var(--color-text)",
            borderColor: focused ? "var(--color-primary)" : "var(--color-border-light)",
            outline: "none",
          }}
        />
        <button
          type="submit"
          disabled={!canSave}
          className="rounded-[var(--radius-md)] px-4 py-2 text-sm font-medium disabled:opacity-50"
          style={{
            backgroundColor: "var(--color-primary)",
            color: "var(--color-text-inverse)",
          }}
        >
          {saving ? RESIDENCY_PROMPT.saving : RESIDENCY_PROMPT.save}
        </button>
      </div>

      {/*
        Counted in characters rather than bytes — this bound is written in
        characters on the server — and only once the limit is in sight, on the
        same reasoning as the report dialog's counter: the number is a rule, not
        a target, and showing it against "by the mill" is noise.
      */}
      {chars > MAX_RESIDENCY_CLAIM_LENGTH / 2 && (
        <p className="mt-1 text-xs" style={{ color: "var(--color-text-tertiary)" }}>
          {chars}/{MAX_RESIDENCY_CLAIM_LENGTH} characters
        </p>
      )}

      {!check.valid && (
        <p className="mt-1 text-xs" style={{ color: "var(--color-danger)" }}>
          {check.error}
        </p>
      )}

      <p className="mt-2 text-xs leading-relaxed" style={{ color: "var(--color-text-tertiary)" }}>
        {RESIDENCY_PROMPT.privacy}
      </p>

      {/*
        A 204 is the whole confirmation the server offers, so the acknowledgement
        is written here — and announced, because a quiet save is otherwise
        invisible to anybody not watching the button.
      */}
      {saved !== null && !error && (
        <p className="mt-2 text-xs" style={{ color: "var(--color-success)" }} role="status">
          {RESIDENCY_PROMPT.saved}
        </p>
      )}

      {error && (
        <div className="mt-2">
          <ErrorBanner message={error} />
        </div>
      )}
    </form>
  );
}
