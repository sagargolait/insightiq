"use client";

// TanStack Query hooks over the typed client in ./api. Components fetch and
// mutate through these; mutations keep the cache in sync so screens update
// without manual reloads.

import { QueryCache, QueryClient, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, type Invitation, type Member } from "@/lib/api";
import type { Role } from "@/lib/permissions";

declare module "@tanstack/react-query" {
  interface Register {
    defaultError: ApiError | Error;
    queryMeta: {
      /** Don't bounce to /login on 401 (the caller handles signed-out state). */
      authOptional?: boolean;
    };
  }
}

export const queryKeys = {
  me: ["auth", "me"] as const,
  orgs: ["orgs"] as const,
  members: (orgId: string) => ["orgs", orgId, "members"] as const,
  invitations: (orgId: string) => ["orgs", orgId, "invitations"] as const,
};

const isClientError = (err: unknown) => err instanceof ApiError && err.status >= 400 && err.status < 500;

export function makeQueryClient() {
  return new QueryClient({
    queryCache: new QueryCache({
      // Session is gone (expired, revoked, or cookie cleared): send to login.
      onError: (err, query) => {
        if (query.meta?.authOptional) return;
        if (err instanceof ApiError && err.status === 401 && typeof window !== "undefined") {
          window.location.replace(`/login?next=${encodeURIComponent(window.location.pathname)}`);
        }
      },
    }),
    defaultOptions: {
      queries: {
        staleTime: 30_000,
        refetchOnWindowFocus: false,
        // 4xx won't succeed on retry; only retry network/5xx failures.
        retry: (count, err) => !isClientError(err) && count < 2,
      },
      mutations: { retry: false },
    },
  });
}

// ---- Queries ---------------------------------------------------------------

export function useMe(opts: { authOptional?: boolean } = {}) {
  return useQuery({
    queryKey: queryKeys.me,
    queryFn: api.auth.me,
    meta: { authOptional: opts.authOptional },
    retry: opts.authOptional ? false : undefined,
  });
}

export function useOrgs() {
  return useQuery({ queryKey: queryKeys.orgs, queryFn: api.orgs.list });
}

export function useMembers(orgId: string) {
  return useQuery({ queryKey: queryKeys.members(orgId), queryFn: () => api.orgs.members(orgId) });
}

export function useInvitations(orgId: string, { enabled = true }: { enabled?: boolean } = {}) {
  return useQuery({
    queryKey: queryKeys.invitations(orgId),
    queryFn: () => api.orgs.invitations(orgId),
    enabled,
  });
}

// ---- Auth mutations --------------------------------------------------------

export function useLogin() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.auth.login,
    // New identity: drop anything cached for a previous user.
    onSuccess: (user) => {
      qc.clear();
      qc.setQueryData(queryKeys.me, user);
    },
  });
}

export function useRegister() {
  return useMutation({ mutationFn: api.auth.register });
}

export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.auth.logout,
    // Clear even on failure: the user asked to sign out.
    onSettled: () => qc.clear(),
  });
}

export function useAcceptInvitation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.invitations.accept,
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.orgs }),
  });
}

// ---- Org mutations ---------------------------------------------------------

export function useCreateOrg() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.orgs.create,
    onSuccess: () => qc.invalidateQueries({ queryKey: queryKeys.orgs, exact: true }),
  });
}

export function useChangeRole(orgId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, role }: { userId: string; role: Role }) => api.orgs.changeRole(orgId, userId, role),
    onSuccess: (updated, { userId }) =>
      qc.setQueryData<Member[]>(queryKeys.members(orgId), (list) =>
        list?.map((m) => (m.user_id === userId ? { ...m, ...updated } : m)),
      ),
  });
}

export function useRemoveMember(orgId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ userId }: { userId: string; self?: boolean }) => api.orgs.removeMember(orgId, userId),
    onSuccess: (_, { userId, self }) => {
      if (self) return qc.invalidateQueries({ queryKey: queryKeys.orgs, exact: true });
      qc.setQueryData<Member[]>(queryKeys.members(orgId), (list) => list?.filter((m) => m.user_id !== userId));
    },
  });
}

export function useInvite(orgId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: { email: string; role: Role }) => api.orgs.invite(orgId, data),
    onSuccess: (created) => {
      // Keep the one-time token out of cached list state.
      const inv: Invitation = { ...created };
      delete (inv as Partial<typeof created>).token;
      qc.setQueryData<Invitation[]>(queryKeys.invitations(orgId), (list) => (list ? [inv, ...list] : list));
    },
  });
}

export function useRevokeInvitation(orgId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (invitationId: string) => api.orgs.revokeInvitation(orgId, invitationId),
    onSuccess: (_, invitationId) =>
      qc.setQueryData<Invitation[]>(queryKeys.invitations(orgId), (list) =>
        list?.filter((i) => i.id !== invitationId),
      ),
  });
}
