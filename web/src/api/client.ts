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
}

export const api = new ApiClient();

// Convenience wrappers used by context providers and hooks.
export const userApi = {
  getMe: () => api.get<import("./types").User>("/me"),
};

export const moderationApi = {
  getModerationQueue: (limit: number, offset: number) =>
    api.get<import("./types").ModerationQueueResponse>(
      `/moderation/queue?limit=${limit}&offset=${offset}`,
    ),
  updateReportStatus: (reportId: string, status: string) =>
    api.patch<import("./types").Report>(`/moderation/reports/${reportId}`, {
      status,
    }),
  takeAction: (req: import("./types").TakeActionRequest) =>
    api.post<import("./types").ActionHistoryEntry>("/moderation/actions", req),
  getActionHistory: (userId: string, limit: number, offset: number) =>
    api.get<import("./types").ActionHistoryResponse>(
      `/moderation/actions/${userId}?limit=${limit}&offset=${offset}`,
    ),
  getPost: (postId: string) =>
    api.get<import("./types").Post>(`/posts/${postId}`),
};

export const configApi = {
  getConfig: () => api.get<import("./types").TownConfig>("/config"),
  updateConfig: (config: Record<string, string>) =>
    api.put<void>("/admin/config", config),
};
