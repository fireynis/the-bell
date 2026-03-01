import type { ApiError } from "./types";

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

  patch<T>(path: string, body: unknown): Promise<T> {
    return this.request<T>(path, {
      method: "PATCH",
      body: JSON.stringify(body),
    });
  }

  delete(path: string): Promise<void> {
    return this.request<void>(path, { method: "DELETE" });
  }
}

export const api = new ApiClient();

// --- Typed API helpers ---

import type {
  User,
  Post,
  QueueResponse,
  TakeActionRequest,
  TakeActionResult,
  Report,
  ActionsResponse,
} from "./types";

export const moderationApi = {
  getMe: () => api.get<User>("/me/"),
  getPost: (id: string) => api.get<Post>(`/posts/${id}`),
  getModerationQueue: (limit = 20, offset = 0) =>
    api.get<QueueResponse>(`/moderation/queue?limit=${limit}&offset=${offset}`),
  takeAction: (req: TakeActionRequest) =>
    api.post<TakeActionResult>("/moderation/actions", req),
  updateReportStatus: (reportId: string, status: string) =>
    api.patch<Report>(`/moderation/reports/${reportId}`, { status }),
  getActionHistory: (userId: string, limit = 20, offset = 0) =>
    api.get<ActionsResponse>(
      `/moderation/actions/${userId}?limit=${limit}&offset=${offset}`,
    ),
};
