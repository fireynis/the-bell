import type {
  ActionHistoryEntry,
  ActionHistoryResponse,
  ApiError,
  CreatePostRequest,
  CreateVouchRequest,
  DirectoryResponse,
  ModerationQueueResponse,
  MuteStatus,
  OwnModerationHistoryResponse,
  Post,
  Report,
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
   * Answers 409 once the window has closed; see postMutationErrorMessage.
   */
  update: (postId: string, body: string) => api.patch<Post>(`/posts/${postId}`, { body }),

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
};

export const configApi = {
  getConfig: () => api.get<TownConfig>("/config"),
  updateConfig: (config: Record<string, string>) => api.put<void>("/admin/config", config),
};
