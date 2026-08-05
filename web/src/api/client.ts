import type {
  ActionHistoryEntry,
  ActionHistoryResponse,
  ApiError,
  CreatePostRequest,
  CreateVouchRequest,
  ModerationQueueResponse,
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
      throw { error: body.error ?? res.statusText, status: res.status } satisfies ApiError;
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
      throw { error: body.error || res.statusText, status: res.status } satisfies ApiError;
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
};

export const vouchApi = {
  listForUser: (userId: string) => api.get<VouchesResponse>(`/users/${userId}/vouches`),

  // NOTE: as of this writing neither of the two below has a server route.
  // VouchService.Vouch and VouchService.Revoke exist in
  // internal/service/vouch.go but nothing in internal/server/routes.go reaches
  // them — the /v1/vouches group is the council approval flow (GET /pending,
  // POST /approve/{id}) and is guarded to RoleCouncil. These paths follow the
  // design doc and the service signatures; confirm them against the handler
  // once it lands, because the two disagree about revoke: the design doc keys
  // it by vouchee id, while VouchService.Revoke takes a vouch id.
  create: (req: CreateVouchRequest) => api.post<Vouch>("/vouches", req),
  revoke: (vouchId: string) => api.delete(`/vouches/${vouchId}`),
};

export const postApi = {
  create: (req: CreatePostRequest) => api.post<Post>("/posts/", req),
  /** Multipart variant; the body and image are read from the form data. */
  createWithImage: (form: FormData) => api.upload<Post>("/posts/", form),
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
};

export const configApi = {
  getConfig: () => api.get<TownConfig>("/config"),
  updateConfig: (config: Record<string, string>) => api.put<void>("/admin/config", config),
};
