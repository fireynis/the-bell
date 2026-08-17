export interface Post {
  id: string;
  author_id: string;
  body: string;
  image_path: string;
  /**
   * The author's description of the image, for a reader who cannot see it.
   *
   * Always sent by the server, as the empty string when the post has no image
   * or nobody described it — but typed as optional anyway, because a post can
   * also arrive from the feed cache, which holds JSON marshalled before this
   * field existed. Render it as `post.alt_text ?? ""`: an empty alt is the
   * correct treatment for an undescribed image, while an <img> with no alt
   * attribute is announced by its filename.
   */
  alt_text?: string;
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
  /**
   * Where in town a member says they live, in their own words.
   *
   * It appears on exactly two reads — the council's pending-approval queue, and
   * the member's own profile — and on no other user's profile, no directory row
   * and no post. That is a privacy line the server enforces; treating it as a
   * field like any other is how it would quietly end up on a page it has no
   * business being on.
   *
   * Optional because it is absent from every response that is not one of those
   * two, and because the empty string is a member who cleared it rather than one
   * whose claim was withheld. Read it through residencyClaimOf rather than
   * directly, so a build running against a server that does not send it on the
   * self view degrades to an empty box rather than to `undefined`.
   */
  residency_claim?: string;
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
  /** Rejected with 400 unless the post has an image. */
  alt_text?: string;
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

/**
 * One page of `GET /api/v1/vouches/pending`, longest wait first.
 *
 * `total` counts every applicant matching the search rather than the page, so
 * the dashboard can say how many neighbours are waiting from a request that
 * asked for three of them, and the queue page can end its list without walking
 * off the end to find out. It mirrors DirectoryResponse exactly; the two
 * endpoints share their bounds and their `q` search and differ only in order.
 */
export interface PendingUsersResponse {
  users: User[];
  total: number;
}

/**
 * The three things a council can put to a vote. Used for creating one; a
 * proposal read back off the wire types its `type` as a plain string, because a
 * server that has learned a fourth kind must not white-screen a council page
 * that has not.
 */
export type ProposalType = "council_promotion" | "council_removal" | "bootstrap_reentry";

/**
 * Where a proposal stands. "open" is the only one that can still be voted on;
 * the other two are the record of what the council decided, and a passed
 * proposal has already been carried out by the time it says so.
 */
export type ProposalStatus = "open" | "passed" | "rejected";

/** One proposal as `GET /api/v1/admin/proposals` sends it. */
export interface Proposal {
  id: string;
  /**
   * One of ProposalType. Typed loosely on the read for the reason given there —
   * render it through proposalTitle, which has a sentence for a kind it does not
   * recognise.
   */
  type: string;
  /** Absent for bootstrap re-entry, which is about the town rather than a person. */
  target_user_id?: string;
  /** Absent for the same reason, and for a target who has set no display name. */
  target_display_name?: string;
  /** Why the proposer says the council should do this, in their own words. */
  rationale: string;
  created_by: string;
  /** Absent for a proposer who has set no display name; fall back on anything falsy. */
  created_by_display_name: string;
  status: ProposalStatus;
  created_at: string;
  /** Absent while the proposal is open. */
  decided_at?: string;
  approve_count: number;
  reject_count: number;
  /**
   * The electorate for *this* proposal, which is not always the size of the
   * council: a removal excludes its own target, who does not vote on it. Using
   * a council count from anywhere else would report a tally out of the wrong
   * denominator and, on a small council, name the wrong majority.
   */
  council_size: number;
  /** How the caller voted, or null if they have not — the field is always present. */
  my_vote: "approve" | "reject" | null;
}

export interface ProposalsResponse {
  proposals: Proposal[];
}

/** Body of POST /api/v1/admin/proposals. */
export interface CreateProposalRequest {
  type: ProposalType;
  /** Required for the two targeted types, omitted for bootstrap re-entry. */
  target_user_id?: string;
  /** 1..1000 characters; validate with validateRationale before sending. */
  rationale: string;
}

/**
 * Body of POST /api/v1/admin/proposals/{id}/votes.
 *
 * A boolean rather than the "approve" | "reject" word the tally is counted in,
 * because that is what the server parses. proposalApi.vote takes the word and
 * converts, so no caller has to remember which way round it goes.
 */
export interface CastVoteRequest {
  approve: boolean;
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

/**
 * How a town admits newcomers.
 *
 * "invite" means the only way in is an invitation from someone already here,
 * and the invitation *is* their vouch. "open" is the original behaviour: anyone
 * may register and waits as a pending member until a neighbour vouches for them.
 */
export type RegistrationMode = "invite" | "open";

export interface TownConfig {
  town_name?: string;
  primary_color?: string;
  accent_color?: string;
  /**
   * Typed loosely as the union rather than required, because it is absent from
   * a server that predates invitations and from the defaults the SPA falls back
   * on when /config cannot be read. Read it through registrationMode in
   * lib/invite, which decides what an absent or unrecognised value means.
   */
  registration_mode?: RegistrationMode;
  [key: string]: string | undefined;
}

/**
 * Where an invitation stands. Only "open" can still be accepted or revoked;
 * the other three are the record of how it ended.
 *
 * "accepted" is the one that changed something: the newcomer registered, the
 * inviter's vouch was created, and they arrived as a member rather than as
 * somebody waiting.
 */
export type InviteStatus = "open" | "accepted" | "expired" | "revoked";

/**
 * One invitation as its sender sees it — `GET /api/v1/invites`.
 *
 * The raw token is deliberately not here. It is sent exactly once, in the
 * invite_url of the response that created the invitation, and never again: a
 * listing that carried it would turn "show me who I invited" into "hand me a
 * key to every account I have ever invited".
 */
export interface Invite {
  id: string;
  email: string;
  /** The sender's personal note, or the empty string when they wrote none. */
  note: string;
  /** One of InviteStatus; typed as the union because the server owns all four. */
  status: InviteStatus;
  created_at: string;
  expires_at: string;
  /** Present only on an accepted invitation. */
  consumed_at?: string;
  /**
   * Who the invitation turned into, by name. Absent on an invitation nobody has
   * accepted, and absent for a newcomer who has set no display name — so fall
   * back on anything falsy rather than on `undefined` alone.
   */
  consumed_by_display_name?: string;
}

/** Body of POST /api/v1/invites. The note is optional and bounded at 500 runes. */
export interface CreateInviteRequest {
  email: string;
  note?: string;
}

/**
 * What POST /api/v1/invites answers with.
 *
 * invite_url carries the raw token and is the only time it is ever sent, which
 * is why the dialog puts it in front of the sender to copy rather than
 * mentioning that an email went out and moving on.
 *
 * email_sent false is not a failure of the invitation: the invitation exists
 * and the link works. It means the town has no working mail out, and the sender
 * has to pass the link along themselves.
 */
export interface CreateInviteResponse {
  invite: Invite;
  invite_url: string;
  email_sent: boolean;
  /** Why the mail did not go, when it did not. Absent when email_sent is true. */
  email_error?: string;
}

export interface InvitesResponse {
  invites: Invite[];
}

/**
 * What GET /api/v1/invites/lookup answers for a live invitation — the only
 * invite read that needs no session, because the person reading it does not
 * have one yet.
 *
 * It says who invited them and to where, and nothing else about the town. A
 * consumed, revoked, expired or entirely unknown token is a bare 404 in every
 * case, so the response cannot be used to work out which addresses have been
 * invited.
 */
export interface InviteLookup {
  email: string;
  town_name: string;
  inviter_display_name: string;
  /** Always "open" — a lookup that answers at all is answering about a live invitation. */
  status: "open";
}
