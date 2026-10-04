import { AuthForm } from "@/features/auth/auth-form";
/** Renders sign-in UI; the browser submits credentials to the BFF, which sets HttpOnly cookies. */
export default function LoginPage() { return <AuthForm mode="login" />; }
