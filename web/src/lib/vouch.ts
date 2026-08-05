import type { User, Vouch } from "../api/types";
import { vouchingBlockReason } from "./gating";

/** The subset of the signed-in viewer these checks read. */
type Actor = Pick<User, "id" | "trust_score" | "role" | "is_active"> | null;

/** Mirrors dailyVouchLimit in internal/service/vouch.go. */
export const DAILY_VOUCH_LIMIT = 3;

/**
 * The revocation penalty from docs/plans/2026-02-28-the-bell-design.md: -3 trust
 * for 30 days, to stop vouch-and-revoke gaming while staying small enough that
 * revoking a bad actor is still worth doing.
 *
 * REVOKE_PENALTY_ENFORCED was false while VouchService.Revoke did not apply the
 * penalty — telling someone they will lose trust they will not actually lose is
 * a lie the UI must not tell. It is now true: Revoke writes a trust_penalty row
 * of REVOKE_PENALTY for REVOKE_PENALTY_DAYS, but only when the revoker is the
 * voucher. A moderator revoking someone else's vouch is not penalised, so the
 * warning is shown from the voucher's own view, which is the only place the
 * revoke control appears.
 *
 * Note REVOKE_PENALTY is the raw penalty, not the score drop. The moderation
 * component carries 30% of the composite weight, so -3 moves a composite score
 * by about 0.9 — see TestVouchService_Revoke_CostsUnderOneCompositePoint.
 */
export const REVOKE_PENALTY = 3;
export const REVOKE_PENALTY_DAYS = 30;
export const REVOKE_PENALTY_ENFORCED = true;

/** Milliseconds in a day, for the local-midnight window below. */
const DAY_MS = 86_400_000;

function isVouch(value: unknown): value is Vouch {
  return !!value && typeof (value as Vouch).id === "string";
}

/** startOfLocalDay mirrors startOfDay in internal/service/vouch.go. */
function startOfLocalDay(now: number): number {
  const date = new Date(now);
  date.setHours(0, 0, 0, 0);
  return date.getTime();
}

/**
 * findActiveVouch locates the viewer's existing vouch for someone, which is
 * what turns the action from "vouch" into "revoke".
 *
 * Revoked vouches are ignored: the pair is unique in the database and its
 * status toggles, so a revoked row means the viewer may vouch again rather than
 * that they already have.
 */
export function findActiveVouch(
  vouches: readonly Vouch[] | undefined | null,
  voucherId: string,
  voucheeId: string,
): Vouch | null {
  if (!Array.isArray(vouches)) return null;

  return (
    vouches.find(
      (v) =>
        isVouch(v) &&
        v.status === "active" &&
        v.voucher_id === voucherId &&
        v.vouchee_id === voucheeId,
    ) ?? null
  );
}

/**
 * countVouchesToday counts how many of the viewer's vouches fall inside the
 * server's daily window, so the limit can be explained before it is hit.
 *
 * This is advisory only, and deliberately so. The server counts every vouch row
 * created since midnight, including ones since revoked, while the client is
 * only ever given the active ones — so this can undercount and the server may
 * still refuse. It also measures midnight in the reader's timezone rather than
 * the server's. The server stays the authority; this exists to explain, not to
 * decide.
 */
export function countVouchesToday(
  given: readonly Vouch[] | undefined | null,
  now: number = Date.now(),
): number {
  if (!Array.isArray(given)) return 0;

  const windowStart = startOfLocalDay(now);

  return given.filter((v) => {
    if (!isVouch(v)) return false;
    const created = new Date(v.created_at).getTime();
    if (Number.isNaN(created)) return false;
    return created >= windowStart && created <= now + DAY_MS;
  }).length;
}

/**
 * vouchBlockReason explains why the viewer cannot vouch for this particular
 * person, or returns null when they can.
 *
 * The checks run in the same order as VouchService.Vouch in
 * internal/service/vouch.go, so the reason shown is the one the server would
 * have given. Two of its rules cannot be checked here at all — a cycle in the
 * trust graph, and whether the vouchee exists — which is why the caller must
 * still surface whatever the server says rather than treating a null here as
 * permission.
 */
export function vouchBlockReason(
  viewer: Actor,
  targetUserId: string,
  given: readonly Vouch[] | undefined | null = [],
  now: number = Date.now(),
): string | null {
  if (!viewer) {
    return "You must be signed in to vouch for someone.";
  }

  // Checked before the trust gate because it is the more specific answer: a
  // low-trust member looking at their own profile is not short of trust, they
  // are looking at the wrong person.
  if (viewer.id === targetUserId) {
    return "You cannot vouch for yourself.";
  }

  const gate = vouchingBlockReason(viewer);
  if (gate) return gate;

  if (findActiveVouch(given, viewer.id, targetUserId)) {
    return "You have already vouched for this member.";
  }

  if (countVouchesToday(given, now) >= DAILY_VOUCH_LIMIT) {
    return `You have used all ${DAILY_VOUCH_LIMIT} of today's vouches. You can vouch again tomorrow.`;
  }

  return null;
}

/**
 * canRevokeVouch mirrors the permission check in VouchService.Revoke: the
 * member who gave the vouch may take it back, and moderators and council may
 * revoke anyone's.
 */
export function canRevokeVouch(
  vouch: Pick<Vouch, "voucher_id" | "status"> | null,
  actor: Actor,
): boolean {
  if (!vouch || !actor) return false;
  if (vouch.status !== "active") return false;
  if (!actor.is_active) return false;

  if (vouch.voucher_id === actor.id) return true;
  return actor.role === "moderator" || actor.role === "council";
}

/**
 * describeRevokeCost is the warning shown before a revocation is confirmed.
 *
 * Revoking is not an undo: it removes the trust edge, which lowers the other
 * member's score and can cost them standing they were relying on. The sentence
 * about the revoker's own penalty only appears when the server actually applies
 * one — see REVOKE_PENALTY_ENFORCED.
 *
 * penaltyEnforced is injected rather than read straight from the constant so
 * the wording of that dormant sentence is tested now, while it is being
 * written, rather than first being read by a user on the day someone flips the
 * flag.
 */
export function describeRevokeCost(
  displayName?: string,
  penaltyEnforced: boolean = REVOKE_PENALTY_ENFORCED,
): string {
  const who = displayName?.trim() ? displayName.trim() : "this member";

  const consequences = [
    `Revoking removes your endorsement of ${who} and lowers their trust score.`,
  ];

  if (penaltyEnforced) {
    consequences.push(
      `It also costs you ${REVOKE_PENALTY} trust for ${REVOKE_PENALTY_DAYS} days.`,
    );
  }

  consequences.push(
    `Vouching for them again later counts against your limit of ${DAILY_VOUCH_LIMIT} vouches a day.`,
  );

  return consequences.join(" ");
}
