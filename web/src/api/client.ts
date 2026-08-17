import type {
  ActionHistoryEntry,
  ActionHistoryResponse,
  ApiError,
  CreatePostRequest,
  CreateProposalRequest,
  CreateVouchRequest,
  CreateInviteRequest,
  CreateInviteResponse,
  DirectoryResponse,
  InviteLookup,
  InvitesResponse,
  ModerationQueueResponse,
  MuteStatus,
  OwnModerationHistoryResponse,
  PendingUsersResponse,
  Post,
  Proposal,
  ProposalsResponse,
  Report,
  SuspensionStatus,
  TakeActionRequest,
  TownConfig,
  UpdateProfileRequest,
  User,
  UserPostsResponse,
  Vouch,
  VouchesResponse,
} from "./types";

type ApiErrorListener = (err: ApiError) => void;

const errorListeners = new Set<ApiErrorListener>();

/**
 * onApiError watches every refusal this client raises, and returns the function
 * that stops watching.
 *
 * It exists for the failures that are not the caller's business. An unverified
 * email is refused per request but is a fact about the whole session, and the
 * page that has to say so — the layout — is not the page that made the call. A
 * listener here is what lets one banner speak for all of them; see
 * isEmailUnverified in lib/verification.ts and AuthProvider, its only
 * subscriber.
 *
 * Listeners see the error and cannot stop it: every caller still receives its
 * own rejection and decides for itself what to show.
 */
export function onApiError(listener: ApiErrorListener): () => void {
  errorListeners.add(listener);
  return () => {
    errorListeners.delete(listener);
  };
}

/** Announces a refusal, then hands it back for the caller to throw. */
function announce(err: ApiError): ApiError {
  for (const listener of errorListeners) listener(err);
  return err;
}

class ApiClient {
  private baseUrl: string;

  constructor(baseUrl = "/api/v1") {
    this.baseUrl = baseUrl;
  }

  async request<T>(path: string, options: RequestInit = {}): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const res = await fetch(url, {
      ...options,
      credentials: "include",
      headers: {
        "Content-Type": "application/json",
        ...options.headers,
      },
    });

    if (!res.ok) {
      const body = await res.json().catch(() => ({ error: res.statusText }));
      throw announce({ error: body.error ?? res.statusText, status: res.status });
    }

    if (res.status === 204) return undefined as T;
    return res.json();
  }

  get<T>(path: string): Promise<T> {
    return this.request<T>(path);
  }

  post<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>(path, {
      method: "POST",
      body: JSON.stringify(body),
    });
  }

  put<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>(path, {
      method: "PUT",
      body: JSON.stringify(body),
    });
  }

  patch<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>(path, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  }

  delete(path: string): Promise<void> {
    return this.request<void>(path, { method: "DELETE" });
  }

  async upload<T>(path: string, formData: FormData): Promise<T> {
    const res = await fetch(`${this.baseUrl}${path}`, {
      method: "POST",
      credentials: "include",
      body: formData,
    });
    if (!res.ok) {
      const body = await res.json().catch(() => ({}));
      throw announce({ error: body.error || res.statusText, status: res.status });
    }
    if (res.status === 204) return undefined as T;
    return res.json();
  }
}

export const api = new ApiClient();

// Convenience wrappers used by context providers, hooks and pages. Going
// through these rather than calling api.post with an object literal is what
// gets a request body type-checked against the shape the server parses.
export const userApi = {
  getMe: () => api.get<User>("/me"),
  getProfile: () => api.get<User>("/users/me"),
  getById: (userId: string) => api.get<User>(`/users/${userId}`),
  listPosts: (userId: string, limit: number) =>
    api.get<UserPostsResponse>(`/users/${userId}/posts?limit=${limit}`),
  updateProfile: (req: UpdateProfileRequest) => api.put<User>("/users/me", req),

  /**
   * The moderation taken against the signed-in member, newest first: what was
   * done, why, and what it cost them.
   *
   * The subject is the session, not a parameter — there is no id to pass, which
   * is exactly why there is no way to ask this about anybody else. It names no
   * moderator, and carries no penalty but the member's own.
   *
   * Unlike every other authenticated read here it is answered for a suspended
   * or banned member, who is precisely the person that most needs to see it.
   */
  ownModerationHistory: (limit: number, offset: number) =>
    api.get<OwnModerationHistoryResponse>(
      `/users/me/moderation-history?limit=${limit}&offset=${offset}`,
    ),

  /**
   * The member directory, newest arrival first. Open to any signed-in member
   * including a pending one — finding the neighbour who can vouch for you is
   * the one thing a pending member most needs to be able to do.
   *
   * `q` matches a case-insensitive substring of the display name and is omitted
   * when blank, so an empty search box asks for the whole roll rather than for
   * the members whose name contains nothing. The server caps `limit` at 100.
   *
   * Built through URLSearchParams because the query is typed by a member and
   * may contain spaces, `&` or `#`, all of which would otherwise corrupt the
   * request rather than merely fail to match.
   */
  list: (limit: number, offset: number, q?: string) => {
    const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
    const search = (q ?? "").trim();
    if (search) params.set("q", search);
    return api.get<DirectoryResponse>(`/users?${params.toString()}`);
  },

  /**
   * Records where in town the member says they are, for the council reading
   * their approval.
   *
   * Answers 204 with no body — deliberately, because there is nothing to read
   * back: the claim is exactly the string that was sent, and the one other place
   * it surfaces is a queue this member cannot see. The empty string clears it,
   * which is the only way to take it back.
   *
   * Trimmed here as well as on the server so that the local copy a caller keeps
   * after saving matches what was actually stored, rather than differing by the
   * whitespace the server quietly removed.
   */
  setResidencyClaim: (claim: string) =>
    api.put<void>("/users/me/residency-claim", { claim: (claim ?? "").trim() }),
};

export const approvalApi = {
  /**
   * One page of the council's approval queue, the applicant who has been
   * waiting longest first. Council only, and only while the town is in
   * bootstrap mode — outside it the server answers 403, which is not an error
   * to show anybody: it means the town now admits residents by vouch.
   *
   * `q` matches a case-insensitive substring of the display name and is omitted
   * when blank, exactly as the directory's does. The server caps `limit` at
   * 100. Built through URLSearchParams for the same reason: the term is typed
   * by a council member and may contain characters a bare template string
   * would corrupt.
   */
  listPending: (limit: number, offset: number, q?: string) => {
    const params = new URLSearchParams({ limit: String(limit), offset: String(offset) });
    const search = (q ?? "").trim();
    if (search) params.set("q", search);
    return api.get<PendingUsersResponse>(`/vouches/pending?${params.toString()}`);
  },

  /**
   * Rings an applicant in, answering 200 with the whole updated user.
   *
   * This is the call that can end bootstrap mode: the twentieth approval flips
   * the town out of it, after which the queue itself answers 403. A caller that
   * reloads the queue afterwards has to treat that refusal as "the queue is
   * closed" rather than as a failure.
   */
  approve: (userId: string) => api.post<User>(`/vouches/approve/${userId}`, {}),
};

export const proposalApi = {
  /**
   * The council's proposals, either the ones still open or the ones already
   * decided. The two are separate requests rather than one filtered on the
   * client: the decided list is history and grows without bound, and a page
   * showing three open votes has no business downloading every decision the
   * town has ever made to find them.
   */
  list: (status: "open" | "decided") =>
    api.get<ProposalsResponse>(`/admin/proposals?status=${status}`),

  /** Answers 201 with the created proposal, already open and awaiting votes. */
  create: (req: CreateProposalRequest) => api.post<Proposal>("/admin/proposals", req),

  /**
   * Casts this council member's vote, answering 200 with the whole updated
   * proposal.
   *
   * Read the result rather than adjusting the tally locally: this vote may have
   * been the deciding one, in which case the proposal comes back decided and —
   * for a promotion or a removal — already carried out. There is no second
   * request that tells you that happened.
   */
  vote: (proposalId: string, vote: "approve" | "reject") =>
    api.post<Proposal>(`/admin/proposals/${proposalId}/votes`, { approve: vote === "approve" }),
};

export const vouchApi = {
  listForUser: (userId: string) => api.get<VouchesResponse>(`/users/${userId}/vouches`),

  /** Answers 201 with the created vouch. */
  create: (req: CreateVouchRequest) => api.post<Vouch>("/vouches", req),

  /** Answers 204 with no body; api.request short-circuits before parsing. */
  revoke: (vouchId: string) => api.delete(`/vouches/${vouchId}`),
};

export const postApi = {
  create: (req: CreatePostRequest) => api.post<Post>("/posts/", req),
  /** Multipart variant; the body and image are read from the form data. */
  createWithImage: (form: FormData) => api.upload<Post>("/posts/", form),

  /**
   * Edits an author's own post inside the 15-minute window, answering 200 with
   * the whole updated post — read the body back from the response rather than
   * assuming the sent text, since edited_at only exists on the server's copy.
   *
   * altText is genuinely optional, and passing `undefined` is not the same as
   * passing `""`: the field is left out of the payload entirely, which tells
   * the server to leave the stored description alone. Sending `""` clears it.
   * A caller that always passed the current value back would erase the
   * description of every post whose author edited it from a client that had
   * not loaded one.
   *
   * Answers 409 once the window has closed; see postMutationErrorMessage. A
   * description over 500 runes, or one sent for a post with no image, is a 400.
   */
  update: (postId: string, body: string, altText?: string) =>
    api.patch<Post>(`/posts/${postId}`, altText === undefined ? { body } : { body, alt_text: altText }),

  /**
   * Takes an author's own post down. Answers 204 with no body: the post still
   * exists, with a status only its author and moderators can see, so there is
   * nothing for an ordinary reader to be handed back.
   */
  remove: (postId: string) => api.delete(`/posts/${postId}`),

  /**
   * Files a report against someone else's post, answering 201 with the created
   * report.
   *
   * The reason is bounded in bytes rather than characters — validate it with
   * validateReportReason before calling. A second report on the same post by the
   * same person is a 400, not a 409: the service raises ErrValidation for it.
   */
  report: (postId: string, reason: string) =>
    api.post<Report>(`/posts/${postId}/report`, { reason }),
};

export const moderationApi = {
  getModerationQueue: (limit: number, offset: number) =>
    api.get<ModerationQueueResponse>(`/moderation/queue?limit=${limit}&offset=${offset}`),
  updateReportStatus: (reportId: string, status: string) =>
    api.patch<Report>(`/moderation/reports/${reportId}`, { status }),
  takeAction: (req: TakeActionRequest) =>
    api.post<ActionHistoryEntry>("/moderation/actions", req),
  getActionHistory: (userId: string, limit: number, offset: number) =>
    api.get<ActionHistoryResponse>(`/moderation/actions/${userId}?limit=${limit}&offset=${offset}`),
  getPost: (postId: string) => api.get<Post>(`/posts/${postId}`),

  /**
   * Takes a post down on the moderator's authority, recording why.
   *
   * Answers 204 with no body: the reason is a moderator's private note that
   * domain.Post never serializes, so there is nothing to read back.
   */
  removePost: (postId: string, reason: string) =>
    api.post<void>(`/moderation/posts/${postId}/remove`, { reason }),

  /**
   * When a user's mute expires, for a moderator looking at someone else.
   *
   * This is the only response outside the caller's own profile that carries
   * muted_until, which is why it sits under /moderation rather than on the
   * user's public profile. Without it a moderator's view cannot tell a muted
   * member from any other.
   */
  getMuteStatus: (userId: string) => api.get<MuteStatus>(`/moderation/users/${userId}/mute`),

  /**
   * Lifts a mute before its duration runs out.
   *
   * Answers 204 with no body, including for a user who was not muted: the
   * caller asked for a state and the state holds, the same contract the
   * reaction DELETE has for a reaction that was never left.
   */
  liftMute: (userId: string) => api.delete(`/moderation/users/${userId}/mute`),

  /**
   * When a user's suspension expires, for a moderator looking at someone else.
   *
   * Ask this rather than reading `is_active` off their profile: that flag does
   * read false while a suspension is in force, but it reads false for an
   * account deactivated for any other reason too, and it never says when the
   * suspension ends.
   */
  getSuspensionStatus: (userId: string) =>
    api.get<SuspensionStatus>(`/moderation/users/${userId}/suspension`),

  /**
   * Lifts a suspension before its duration runs out.
   *
   * Answers 204 with no body, including for a user who was not suspended — the
   * same idempotence, for the same reason, as liftMute above.
   */
  liftSuspension: (userId: string) => api.delete(`/moderation/users/${userId}/suspension`),
};

export const inviteApi = {
  /**
   * Invites somebody by email, answering 201 with the invitation, the link that
   * accepts it, and whether the town managed to email it.
   *
   * This is a vouch made in advance — accepting it creates the vouch and lands
   * the newcomer as a member — so it is refused for the same reasons a vouch is:
   * too little trust, and the daily budget the two share. Council are exempt
   * from the budget. A live invitation already out to that address is refused
   * too, and the server's own sentence says which of these it was; pass it
   * through inviteErrorMessage rather than guessing from the status.
   *
   * The note is omitted from the payload entirely when blank, rather than sent
   * as the empty string, so an invitation with no note carries no note.
   */
  create: (req: CreateInviteRequest) =>
    api.post<CreateInviteResponse>("/invites", {
      email: req.email.trim(),
      ...(req.note?.trim() ? { note: req.note.trim() } : {}),
    }),

  /** The caller's own invitations, newest first. Nobody can read anyone else's. */
  list: () => api.get<InvitesResponse>("/invites"),

  /**
   * Withdraws an invitation that has not been accepted yet. Answers 204 with no
   * body — the invitation still exists, revoked, and there is nothing about that
   * the sender does not already know.
   */
  revoke: (inviteId: string) => api.delete(`/invites/${inviteId}`),

  /**
   * Reads a raw invitation token, for the registration page — the one invite
   * call that works without a session, since the person following the link does
   * not have one.
   *
   * A 404 is the answer for consumed, revoked, expired and unknown tokens
   * alike, deliberately: distinguishing them would let anyone with a list of
   * addresses learn which of them the town has invited. Treat it as "this link
   * is no longer good" and nothing more precise.
   *
   * Built through URLSearchParams because the token comes off a URL the visitor
   * was sent and cannot be trusted to be free of characters that would corrupt
   * the query.
   */
  lookup: (token: string) =>
    api.get<InviteLookup>(`/invites/lookup?${new URLSearchParams({ token }).toString()}`),
};

export const configApi = {
  getConfig: () => api.get<TownConfig>("/config"),
  updateConfig: (config: Record<string, string>) => api.put<void>("/admin/config", config),
};
