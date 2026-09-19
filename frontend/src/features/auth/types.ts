export type User = { id: string; email: string; created_at: string };
export type Action = "login" | "register" | "session" | "logout";
export type AuthResult = { user: User };
