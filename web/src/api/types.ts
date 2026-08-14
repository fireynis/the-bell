export interface Post {
  id: string;
  author_id: string;
  body: string;
  image_path: string;
  status: string;
  created_at: string;
  edited_at: string | null;
  author_display_name?: string;
  author_avatar_url?: string;
  reaction_counts?: Record<string, number>;
  user_reactions?: string[];
}

/**
 * One entry in a member's own record of being released from a mute early.
 *
 * It names no moderator. Which moderator acted appears on no member-facing
 * response — the member-facing history at /users/me/moderation-history holds
 * that line too — and changing it is a policy decision rather than a property
 * of this record.
 */
export interface MuteLift {
  lifted_at: string;
  /** When the mute would have ended had it run its course; absent if unknown. */
  previous_muted_until?: string;
}

/**
 * One entry in a member's own record of being released from a suspension early.
 *
 * Separate from MuteLift, and not merged with it, because the two carry
 * different "would have ended" fields and mean different things to the member.
 * It names no moderator either, for the same reason MuteLift does not.
 */
export interface SuspensionLift {
  lifted_at: string;
  /**
   * When the suspension would have ended had it run its course; absent if
   * unknown.
   */
  previous_suspended_until?: string;
}

export interface User {
  id: string;
  display_name: string;
  bio: string;
  avatar_url: string;
  trust_score: number;
  role: string;
  is_active: boolean;
  joined_at: string;
  /**
   * Present only on the caller's own profile, and only while a mute is in
   * force: absent — never null — otherwise, so the field's presence is the
   * whole answer to "am I muted?". A mute is between the member and the
   * moderators, so it appears on no other user's profile.
   */
  muted_until?: string;
  /**
   * Mutes a moderator ended early, newest first, and only on the caller's own
   * profile. Absent rather than empty when there are none.
   *
   * It exists because muted_until vanishes the moment a mute is lifted, so
   * without it a member released early has no way to learn that it happened.
   * The actions taken against them are a separate read —
   * userApi.ownModerationHistory — because a lift is not one of those: it
   * carries no severity and costs no trust.
   */
  mute_lifts?: MuteLift[];
  /**
   * Suspensions a moderator ended early, newest first, and only on the caller's
   * own profile. Absent rather than empty when there are none.
   *
   * This is the only trace of an early release a member can see. A suspension
   * reaches them as `is_active` being false, and lifting it simply makes that
   * revert — so without this there is nothing to tell an early release from a
   * suspension that ran its full course.
   */
  suspension_lifts?: SuspensionLift[];
}

/**
 * One row of the member directory — `GET /api/v1/users`.
 *
 * Deliberately not a `User`. The directory is a list anybody signed in may
 * read, so it carries only what a list of people needs: who they are, where
 * they stand, and how long they have been here. Trust scores, bios and
 * moderation state are on the profile, which is a read about one person rather
 * than about everybody.
 */
export interface DirectoryUser {
  id: string;
  /**
   * The empty string for a member who has set no name — the key is always
   * present. Render it through personName rather than directly.
   */
  display_name: string;
  /** "pending" | "member" | "moderator" | "council"; typed loosely because the
   * server may introduce a role this build has never heard of. */
  role: string;
  joined_at: string;
}

export interface DirectoryResponse {
  users: DirectoryUser[];
  /**
   * How many members match the search, ignoring limit and offset. This is the
   * only way to tell a complete list from the first page of a longer one.
   */
  total: number;
}

export interface FeedResponse {
  posts: Post[];
  next_cursor?: string;
}

export interface CreatePostRequest {
  body: string;
  image_path?: string;
}

export interface Vouch {
  id: string;
  voucher_id: string;
  vouchee_id: string;
  /**
   * Both parties by name, on the profile listing only. The create response
   * (POST /api/v1/vouches) carries neither, which is why these are optional
   * rather than required — and a member who has set no display name sends the
   * empty string, so fall back to the id for anything falsy, not just for
   * `undefined`.
   */
  voucher_display_name?: string;
  vouchee_display_name?: string;
  status: string;
  created_at: string;
  /**
   * Present only on a revoked vouch. Optional rather than nullable because the
   * Go field is `*time.Time` with omitempty, so it is absent rather than null —
   * and the profile listing's vouchEntry DTO never sends it at all.
   */
  revoked_at?: string;
}

export interface VouchesResponse {
  received: Vouch[];
  given: Vouch[];
}

/** Body of POST /api/v1/vouches; mirrors the vouchee argument of VouchService.Vouch. */
export interface CreateVouchRequest {
  vouchee_id: string;
}

export interface UserPostsResponse {
  posts: Post[];
}

export interface UpdateProfileRequest {
  display_name: string;
  bio: string;
  avatar_url: string;
}

export interface ApiError {
  error: string;
  status: number;
}

export interface TownStats {
  total_users: number;
  posts_today: number;
  active_moderators: number;
  pending_users: number;
}

export interface CouncilVote {
  id: string;
  proposal_id: string;
  voter_id: string;
  vote: "approve" | "reject";
  created_at: string;
}

export interface ProposalSummary {
  proposal_id: string;
  approve_count: number;
  reject_count: number;
  total_council: number;
  status: "pending" | "approved" | "rejected";
  votes: CouncilVote[];
}

export interface PendingUsersResponse {
  users: User[];
}

export interface ProposalsResponse {
  proposals: ProposalSummary[];
}

export interface Report {
  id: string;
  reporter_id: string;
  /**
   * Who filed the report, on the moderation queue only — the report echoed back
   * to its own reporter carries no name. Also absent for a member who has set
   * no display name, so fall back to the id for anything falsy.
   */
  reporter_display_name?: string;
  post_id: string;
  reason: string;
  status: string;
  created_at: string;
}

export interface ModerationQueueResponse {
  reports: Report[];
}

export interface TrustPenalty {
  id: string;
  user_id: string;
  moderation_action_id: string;
  penalty_amount: number;
  hop_depth: number;
  created_at: string;
  decays_at: string | null;
}

export interface ModerationAction {
  id: string;
  target_user_id: string;
  /**
   * Both parties by name, on the audit-trail listing only. The action echoed
   * back by POST /api/v1/moderation/actions carries neither. Also absent for a
   * member who has set no display name, so fall back to the id for anything
   * falsy.
   */
  target_display_name?: string;
  moderator_id: string;
  moderator_display_name?: string;
  action: string;
  severity: number;
  reason: string;
  duration: number | null;
  created_at: string;
  expires_at: string | null;
}

export interface ActionHistoryEntry {
  action: ModerationAction;
  penalties: TrustPenalty[];
}

export interface ActionHistoryResponse {
  actions: ActionHistoryEntry[];
}

/**
 * What one moderation action cost the member it landed on — and nothing about
 * what it cost anyone else.
 */
export interface OwnPenalty {
  /** Trust points the member themselves lost. */
  amount: number;
  /**
   * When the penalty has faded away completely. Absent — never null — when it
   * never does, which is true of a ban's penalty and of nothing else.
   *
   * That reading is only unambiguous because the enclosing `penalty` object is
   * present at all: an action whose penalty was never recorded carries no
   * `penalty` key, so "permanent" and "none" cannot be confused.
   */
  decays_at?: string;
}

/**
 * One moderation action as its subject sees it — `GET
 * /api/v1/users/me/moderation-history`.
 *
 * Deliberately not a `ModerationAction`. That type is the moderator's view of
 * the audit trail and carries `moderator_id` and `moderator_display_name`;
 * which moderator handled a case appears on no member-facing response, and the
 * separate type is what keeps the two from drifting into one.
 */
export interface OwnModerationEntry {
  id: string;
  /** "warn" | "mute" | "suspend" | "ban". A string, so a new server-side action type cannot break the page. */
  action: string;
  severity: number;
  /** The moderator's written reason, exactly as they wrote it. */
  reason: string;
  created_at: string;
  /**
   * When the restriction the action imposed ends. Absent for a warning and for
   * a ban, neither of which expires.
   *
   * It is the expiry recorded at the time, so a mute a moderator later lifted
   * early still reports the expiry it was given — the lift itself reaches the
   * member through `mute_lifts` on their profile.
   */
  expires_at?: string;
  /** Absent when no direct penalty was recorded against the member. */
  penalty?: OwnPenalty;
}

export interface OwnModerationHistoryResponse {
  actions: OwnModerationEntry[];
}

/**
 * What GET /api/v1/moderation/users/{id}/mute answers with.
 *
 * muted_until is absent — never null — for a user who is not muted, and for one
 * whose mute has expired: the field's presence is the answer. That mirrors the
 * caller's own profile, the only other response that carries it. Read it
 * through activeMuteExpiry rather than testing the field directly.
 */
export interface MuteStatus {
  muted_until?: string;
}

/**
 * What GET /api/v1/moderation/users/{id}/suspension answers with.
 *
 * Same shape and same rule as MuteStatus one severity up: suspended_until is
 * absent — never null — for a user who is not suspended, and for one whose
 * suspension has lapsed, so the field's presence is the answer.
 *
 * Ask this rather than reading `is_active` on a profile. That flag does read
 * false during a suspension, but it reads false for a deactivated account too
 * and never says when the suspension ends.
 */
export interface SuspensionStatus {
  suspended_until?: string;
}

export interface TakeActionRequest {
  target_user_id: string;
  action_type: string;
  severity: number;
  reason: string;
  duration_seconds?: number;
}

export interface TownConfig {
  town_name?: string;
  primary_color?: string;
  accent_color?: string;
  [key: string]: string | undefined;
}
