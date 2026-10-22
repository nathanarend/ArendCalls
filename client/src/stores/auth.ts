import { create } from "zustand";

type AuthStatus = "checking" | "login" | "ready";

type State = {
  status: AuthStatus;
  /** false = servidor sem ADMIN_PASSWORD_HASH: painel aberto, sem login. */
  authRequired: boolean;
  /** true = sessão caiu com o painel já carregado; após o login, recarrega. */
  expired: boolean;
};

export const useAuth = create<State>(() => ({ status: "checking", authRequired: false, expired: false }));

export const checkAuth = async (): Promise<void> => {
  try {
    const r = await fetch("/api/auth/status");
    const st = (await r.json()) as { authRequired: boolean; authenticated: boolean };
    const ok = !st.authRequired || st.authenticated;
    useAuth.setState((s) => ({
      authRequired: st.authRequired,
      status: ok ? "ready" : "login",
      expired: s.expired || (!ok && s.status === "ready"),
    }));
  } catch {
    // Servidor fora do ar: mantém o painel e deixa as chamadas falharem.
    useAuth.setState({ status: "ready" });
  }
};

/** Resposta 401 da API: com login ligado, volta para a tela de login. */
export const markUnauthorized = (): void => {
  const { authRequired, status } = useAuth.getState();
  if (authRequired && status === "ready") useAuth.setState({ status: "login", expired: true });
};

export class LoginError extends Error {
  constructor(message: string, readonly retryAfter?: number) {
    super(message);
  }
}

export const login = async (username: string, password: string): Promise<void> => {
  const r = await fetch("/api/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  });
  if (!r.ok) {
    const body = (await r.json().catch(() => ({}))) as { error?: string; retryAfter?: number };
    throw new LoginError(body.error ?? `Falha no login (${r.status})`, body.retryAfter);
  }
  // Sessão caiu no meio do uso: recarrega para religar eventos e listas do zero.
  if (useAuth.getState().expired) {
    window.location.reload();
    return;
  }
  useAuth.setState({ status: "ready" });
};

export const logout = async (): Promise<void> => {
  await fetch("/api/auth/logout", { method: "POST" }).catch(() => {});
  window.location.reload();
};
