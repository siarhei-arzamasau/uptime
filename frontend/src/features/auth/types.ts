export type User = { id: string; email: string; name: string; avatar_url: string; created_at: string };
export type Action = "login" | "register" | "session" | "logout" | "profile" | "profile-update" | "avatar";
export type AuthResult = { user: User };
