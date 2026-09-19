// @vitest-environment jsdom
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { expect, it, vi } from "vitest";
import { ThemeToggle } from "./theme-toggle";
it("toggles and persists a manual theme", async () => {
  render(<ThemeToggle initialTheme="light" />); await userEvent.click(screen.getByRole("button", { name: "Toggle color theme" })); expect(document.documentElement.dataset.theme).toBe("dark"); expect(document.cookie).toContain("uptime_theme=dark"); await userEvent.click(screen.getByRole("button", { name: "Toggle color theme" })); expect(document.documentElement.dataset.theme).toBe("light");
});
it("uses the system theme before an explicit choice", async () => {
  vi.stubGlobal("matchMedia", vi.fn(() => ({ matches: true }))); render(<ThemeToggle initialTheme="system" />); await userEvent.click(screen.getByRole("button", { name: "Toggle color theme" })); expect(document.documentElement.dataset.theme).toBe("light"); vi.unstubAllGlobals();
});
