// Typed client for services/api. Shapes mirror the Go models in
// internal/modules/{auth,organizations}/model.go.

import type { Role } from "@/lib/permissions";

export type User = {
  id: string;
  name: string;
  email: string;
  status: string;
  created_at: string;
};

export type Organization = {
  id: string;
  name: string;
  slug: string;
  status: string;
  created_at: string;
};

export type OrganizationWithRole = Organization & { role: Role };

export type Member = {
  user_id: string;
  name: string;
  email: string;
  role: Role;
  joined_at: string;
};

export type Invitation = {
  id: string;
  organization_id: string;
  email: string;
  role: Role;
  invited_by: string | null;
  expires_at: string;
  created_at: string;
};

/** Returned once at creation; the raw token is never retrievable again. */
export type CreatedInvitation = Invitation & { token: string };

export type FieldError = { field: string; message: string };

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly fields: FieldError[] = [],
    readonly requestId?: string,
  ) {
    super(message);
    this.name = "ApiError";
  }

  fieldError(field: string): string | undefined {
    return this.fields.find((f) => f.field === field)?.message;
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const res = await fetch(`/api/v1${path}`, {
    ...init,
    credentials: "same-origin",
    headers: {
      ...(init.body ? { "Content-Type": "application/json" } : {}),
      ...init.headers,
    },
  });

  if (res.status === 204) return undefined as T;

  const body = await res.json().catch(() => null);

  if (!res.ok) {
    const err = body?.error;
    throw new ApiError(
      res.status,
      err?.code ?? "UNKNOWN_ERROR",
      err?.message ?? `Request failed (${res.status})`,
      err?.fields ?? [],
      err?.request_id,
    );
  }

  return (body?.data ?? body) as T;
}

const json = (method: string, data?: unknown): RequestInit => ({
  method,
  body: data === undefined ? undefined : JSON.stringify(data),
});

export const api = {
  auth: {
    register: (data: { name: string; email: string; password: string }) =>
      request<User>("/auth/register", json("POST", data)),
    login: (data: { email: string; password: string }) =>
      request<User>("/auth/login", json("POST", data)),
    logout: () => request<void>("/auth/logout", json("POST")),
    me: () => request<User>("/auth/me"),
  },
  orgs: {
    list: () => request<OrganizationWithRole[]>("/orgs"),
    get: (orgId: string) => request<OrganizationWithRole>(`/orgs/${orgId}`),
    create: (data: { name: string; slug: string }) =>
      request<Organization>("/orgs", json("POST", data)),

    members: (orgId: string) => request<Member[]>(`/orgs/${orgId}/members`),
    changeRole: (orgId: string, userId: string, role: Role) =>
      request<Member>(`/orgs/${orgId}/members/${userId}`, json("PATCH", { role })),
    removeMember: (orgId: string, userId: string) =>
      request<void>(`/orgs/${orgId}/members/${userId}`, json("DELETE")),

    invitations: (orgId: string) => request<Invitation[]>(`/orgs/${orgId}/invitations`),
    invite: (orgId: string, data: { email: string; role: Role }) =>
      request<CreatedInvitation>(`/orgs/${orgId}/invitations`, json("POST", data)),
    revokeInvitation: (orgId: string, invitationId: string) =>
      request<void>(`/orgs/${orgId}/invitations/${invitationId}`, json("DELETE")),
  },
  invitations: {
    accept: (token: string) =>
      request<Organization>("/invitations/accept", json("POST", { token })),
  },
};
