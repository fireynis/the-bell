import type { KratosFlow, KratosLogoutFlow, KratosSession, KratosSuccessResponse } from "./kratos-types.ts";

const BASE = "/.ory";

type FlowType = "login" | "registration" | "recovery" | "verification" | "settings";

async function kratosRequest<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    credentials: "include",
    headers: {
      Accept: "application/json",
      ...init?.headers,
    },
  });

  if (!res.ok) {
    const body = await res.json().catch(() => ({ error: { message: res.statusText } }));
    throw { status: res.status, body };
  }

  return res.json();
}

export async function createFlow(type: FlowType): Promise<KratosFlow> {
  return kratosRequest<KratosFlow>(`/self-service/${type}/browser`);
}

export async function getFlow(type: FlowType, id: string): Promise<KratosFlow> {
  return kratosRequest<KratosFlow>(`/self-service/${type}/flows?id=${id}`);
}

export async function submitFlow(
  type: FlowType,
  flowId: string,
  body: Record<string, unknown>,
): Promise<KratosFlow | KratosSuccessResponse> {
  return kratosRequest<KratosFlow | KratosSuccessResponse>(`/self-service/${type}?flow=${flowId}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export async function getSession(): Promise<KratosSession | null> {
  try {
    return await kratosRequest<KratosSession>("/sessions/whoami");
  } catch (e: unknown) {
    const err = e as { status?: number };
    if (err.status === 401) return null;
    throw e;
  }
}

export async function createLogoutFlow(): Promise<KratosLogoutFlow> {
  return kratosRequest<KratosLogoutFlow>("/self-service/logout/browser");
}

export async function performLogout(token: string): Promise<void> {
  await fetch(`${BASE}/self-service/logout?token=${encodeURIComponent(token)}`, {
    credentials: "include",
    headers: { Accept: "application/json" },
  });
}
