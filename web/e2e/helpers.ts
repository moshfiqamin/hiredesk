import { expect, type Page } from "@playwright/test";

/** API is where the HireDesk API listens; the web app calls it from the browser. */
export const API = process.env.E2E_API_URL ?? "http://localhost:8080";

export interface Tokens {
  access_token: string;
  refresh_token: string;
  user: { id: string; email: string; full_name: string; role: string; created_at: string };
}

/** apiLogin signs in through the API directly. For setup only: the flow under test signs in through the UI. */
export async function apiLogin(email: string, password: string): Promise<Tokens> {
  const res = await fetch(`${API}/v1/auth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password }),
  });
  expect(res.ok, `login as ${email}`).toBe(true);
  return (await res.json()) as Tokens;
}

/** apiGet reads a JSON endpoint with a token. */
export async function apiGet<T>(path: string, token?: string): Promise<T> {
  const res = await fetch(`${API}${path}`, { headers: token ? { Authorization: `Bearer ${token}` } : {} });
  expect(res.ok, `GET ${path}`).toBe(true);
  return (await res.json()) as T;
}

/**
 * signInAs puts a session in localStorage before any page script runs,
 * in the shape src/lib/auth/session.ts stores, so a test can open a page
 * already signed in.
 */
export async function signInAs(page: Page, tokens: Tokens) {
  const session = { accessToken: tokens.access_token, refreshToken: tokens.refresh_token, user: tokens.user };
  await page.addInitScript((value) => window.localStorage.setItem("hiredesk.session", value), JSON.stringify(session));
}

/** The development accounts `make seed` creates; their passwords are printed in the README. */
export const seeded = {
  admin: { email: "admin@hiredesk.test", password: "admin-password" },
  interviewer: { email: "interviewer@hiredesk.test", password: "interviewer-password", name: "Ivan Interviewer" },
};
