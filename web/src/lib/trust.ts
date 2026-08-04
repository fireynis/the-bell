import type { User } from "../api/types";

/**
 * Trust thresholds, mirroring internal/domain/user.go. The server is always the
 * authority — these exist so the UI can disable an action the API would reject
 * rather than let someone submit into a guaranteed error.
 */
export const POSTING_THRESHOLD = 30;
export const VOUCHING_THRESHOLD = 60;

export type Role = "pending" | "member" | "moderator" | "council" | "banned";

/** Tiers used to colour the trust bar. */
export type TrustTier = "high" | "medium" | "low";

/**
 * clampTrustScore keeps a score inside the 0-100 range the UI can render. A
 * score outside that range would otherwise produce a bar wider than its track
 * or a negative width.
 *
 * NaN is the one value Math.max/min cannot resolve — it propagates and would
 * render a bar with no width at all — so it is mapped to 0. Infinities clamp
 * to the bounds naturally.
 */
export function clampTrustScore(score: number): number {
  if (Number.isNaN(score)) return 0;
  return Math.max(0, Math.min(100, score));
}

/**
 * trustTier buckets a score by what it lets the user do: "high" means they can
 * vouch, "medium" that they can post, "low" that they can do neither.
 */
export function trustTier(score: number): TrustTier {
  const clamped = clampTrustScore(score);
  if (clamped >= VOUCHING_THRESHOLD) return "high";
  if (clamped >= POSTING_THRESHOLD) return "medium";
  return "low";
}

/** Roles that can never act, whatever their score. */
function isRoleBlocked(role: string): boolean {
  return role === "pending" || role === "banned";
}

export function canPost(user: Pick<User, "trust_score" | "role" | "is_active"> | null): boolean {
  if (!user) return false;
  return user.is_active && !isRoleBlocked(user.role) && user.trust_score >= POSTING_THRESHOLD;
}

export function canVouch(user: Pick<User, "trust_score" | "role" | "is_active"> | null): boolean {
  if (!user) return false;
  return user.is_active && !isRoleBlocked(user.role) && user.trust_score >= VOUCHING_THRESHOLD;
}

export function canModerate(user: Pick<User, "role" | "is_active"> | null): boolean {
  if (!user) return false;
  return user.is_active && (user.role === "moderator" || user.role === "council");
}

export function isCouncil(user: Pick<User, "role" | "is_active"> | null): boolean {
  if (!user) return false;
  return user.is_active && user.role === "council";
}

/**
 * The role hierarchy, ordering the roles declared in internal/domain/user.go.
 * Ranks are ordinal only — the numbers carry no meaning beyond the ordering.
 */
const ROLE_RANK: Record<Role, number> = {
  banned: 0,
  pending: 1,
  member: 2,
  moderator: 3,
  council: 4,
};

/**
 * hasMinRole reports whether a user sits at or above a role in the hierarchy,
 * so nav items and routes share one encoding of it rather than each listing the
 * roles it happens to allow — a list that has to be revisited every time a role
 * is added.
 *
 * An unknown role ranks lowest rather than throwing: the server can introduce a
 * role this build has never heard of, and the safe reading of one is that it
 * grants nothing. Deactivated users are refused whatever their role, matching
 * canModerate and isCouncil.
 */
export function hasMinRole(
  user: Pick<User, "role" | "is_active"> | null,
  minRole: Role,
): boolean {
  if (!user || !user.is_active) return false;
  const userRank = ROLE_RANK[user.role as Role] ?? -1;
  return userRank >= ROLE_RANK[minRole];
}
