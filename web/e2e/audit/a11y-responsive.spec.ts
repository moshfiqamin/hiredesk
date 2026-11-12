import AxeBuilder from "@axe-core/playwright";
import { expect, test, type Page } from "@playwright/test";
import { writeFileSync } from "node:fs";

import { apiGet, apiLogin, seeded, signInAs, type Tokens } from "../helpers";

// The audit behind docs/notes/14-3: every page, at a phone, a tablet and a
// laptop width, in both themes. For each it records axe's WCAG 2.2 A/AA
// violations, whether the page scrolls sideways, and how many h1s it has.
// Run with `npm run audit` against a running stack (`make up && make seed`).

const viewports = [
  { name: "375", width: 375, height: 812 },
  { name: "768", width: 768, height: 1024 },
  { name: "1280", width: 1280, height: 800 },
] as const;
const themes = ["light", "dark"] as const;

interface Target {
  name: string;
  path: string;
  as: "anonymous" | "admin" | "interviewer" | "candidate";
}

interface Finding {
  page: string;
  viewport: string;
  theme: string;
  horizontalScroll: boolean;
  h1Count: number;
  /** Elements that took keyboard focus without any visible indicator (1280px, light only). */
  noFocusIndicator: string[];
  violations: { id: string; impact: string | null | undefined; nodes: number; help: string; where: string[] }[];
}

let targets: Target[] = [];
const sessions: Partial<Record<Target["as"], Tokens>> = {};
const findings: Finding[] = [];

test.beforeAll(async () => {
  sessions.admin = await apiLogin(seeded.admin.email, seeded.admin.password);
  sessions.interviewer = await apiLogin(seeded.interviewer.email, seeded.interviewer.password);

  // A candidate of its own, so the audit never depends on another run's data.
  const email = `audit-${Date.now()}@hiredesk.test`;
  await fetch(`${process.env.E2E_API_URL ?? "http://localhost:8080"}/v1/auth/register`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ email, password: "audit-password", full_name: "Audit Candidate" }),
  });
  sessions.candidate = await apiLogin(email, "audit-password");

  // Real IDs from the running stack: an open role, one of its applicants,
  // and something assigned to the seeded interviewer.
  const listing = await apiGet<{ data: { id: string }[] }>("/v1/job-posts?limit=1");
  const roleId = listing.data[0].id;
  const ranking = await apiGet<{ data: { application_id: string }[] }>(`/v1/job-posts/${roleId}/ranking?limit=1`, sessions.admin.access_token);
  const assigned = await apiGet<{ data: { id: string }[] }>("/v1/applications?limit=1", sessions.interviewer.access_token);

  targets = [
    { name: "Open roles", path: "/roles", as: "anonymous" },
    { name: "Role page", path: `/roles/${roleId}`, as: "anonymous" },
    { name: "Sign in", path: "/login", as: "anonymous" },
    { name: "Create account", path: "/register", as: "anonymous" },
    { name: "Application form", path: `/roles/${roleId}/apply`, as: "candidate" },
    { name: "My applications", path: "/my-applications", as: "candidate" },
    { name: "My assignments", path: "/assignments", as: "interviewer" },
    ...(assigned.data[0] ? [{ name: "Review page", path: `/assignments/${assigned.data[0].id}`, as: "interviewer" as const }] : []),
    { name: "Admin roles", path: "/admin/roles", as: "admin" },
    { name: "New role", path: "/admin/roles/new", as: "admin" },
    { name: "Role overview", path: `/admin/roles/${roleId}`, as: "admin" },
    { name: "Role details", path: `/admin/roles/${roleId}/edit`, as: "admin" },
    { name: "Questions", path: `/admin/roles/${roleId}/questions`, as: "admin" },
    { name: "Rubric", path: `/admin/roles/${roleId}/rubric`, as: "admin" },
    { name: "Ranking", path: `/admin/roles/${roleId}/ranking`, as: "admin" },
    { name: "Applications", path: "/admin/applications", as: "admin" },
    ...(ranking.data[0] ? [{ name: "Candidate", path: `/admin/applications/${ranking.data[0].application_id}`, as: "admin" as const }] : []),
  ];
});

test.afterAll(() => {
  writeFileSync("test-results/a11y-responsive.json", JSON.stringify(findings, null, 2));
});

async function audit(page: Page, target: Target, viewport: (typeof viewports)[number], theme: (typeof themes)[number]) {
  await page.setViewportSize({ width: viewport.width, height: viewport.height });
  await page.emulateMedia({ colorScheme: theme, reducedMotion: "reduce" });
  // next-themes reads its choice from localStorage before the first paint.
  await page.addInitScript((t) => window.localStorage.setItem("theme", t), theme);
  if (target.as !== "anonymous") await signInAs(page, sessions[target.as]!);

  await page.goto(target.path);
  // Settled: one h1 rendered and no skeleton still standing in for content.
  await expect(page.locator("h1").first()).toBeVisible();
  await expect(page.locator('[aria-busy="true"]')).toHaveCount(0, { timeout: 10_000 });

  const horizontalScroll = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
  const h1Count = await page.locator("h1").count();
  // Keyboard sweep, at one width and theme to keep the run short: press Tab
  // through the page and record any element that takes focus without an
  // outline, a non-transparent box-shadow ring, or a border that changes.
  const noFocusIndicator: string[] = [];
  if (viewport.name === "1280" && theme === "light") {
    const seen = new Set<string>();
    for (let i = 0; i < 60; i++) {
      await page.keyboard.press("Tab");
      const info = await page.evaluate(async () => {
        // Two frames, so the focus styles of this keypress have been applied
        // before they are measured.
        await new Promise((resolve) => requestAnimationFrame(() => requestAnimationFrame(resolve)));
        const el = document.activeElement as HTMLElement | null;
        if (!el || el === document.body) return null;
        const shows = (node: Element) => {
          const style = getComputedStyle(node);
          const ring = style.boxShadow.split(/,(?![^(]*\))/).some((part) => part.trim() !== "none" && !/rgba\(0, 0, 0, 0\)/.test(part));
          const outline = style.outlineStyle !== "none" && parseFloat(style.outlineWidth) > 0;
          return ring || outline;
        };
        // The indicator may be drawn by the element or by a wrapper styled
        // with has-[:focus-visible] (a card around its link, a label around
        // its radio), so the element and three ancestors are checked.
        let visible = false;
        for (let node: Element | null = el, depth = 0; node && depth < 4; node = node.parentElement, depth++) {
          if (shows(node)) visible = true;
        }
        const name = el.getAttribute("aria-label") || el.textContent?.trim().slice(0, 40) || el.tagName;
        return { key: `${el.tagName.toLowerCase()} "${name}"`, visible };
      });
      if (!info || seen.has(info.key)) break;
      seen.add(info.key);
      if (!info.visible) noFocusIndicator.push(info.key);
    }
  }

  const results = await new AxeBuilder({ page }).withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa", "wcag22aa"]).analyze();

  const finding: Finding = {
    page: target.name,
    viewport: viewport.name,
    theme,
    horizontalScroll,
    h1Count,
    noFocusIndicator,
    violations: results.violations.map((v) => ({
      id: v.id,
      impact: v.impact,
      nodes: v.nodes.length,
      help: v.help,
      // Which elements, and axe's own reason (for contrast: the two colours and the ratio).
      where: v.nodes.map((n) => `${n.target.join(" ")} :: ${n.failureSummary ?? ""}`),
    })),
  };
  findings.push(finding);

  expect.soft(horizontalScroll, `${target.name} scrolls sideways at ${viewport.name}px`).toBe(false);
  expect.soft(h1Count, `${target.name} has ${h1Count} h1 elements`).toBe(1);
  expect.soft(noFocusIndicator, `${target.name}: focused without a visible indicator`).toEqual([]);
  expect.soft(finding.violations, `${target.name} at ${viewport.name}px, ${theme}`).toEqual([]);
}

// One test, so the setup above (three sign-ins and a registration) runs once:
// the API allows ten auth requests a minute per address, and a test per
// combination would spend that budget every time a worker restarted. Soft
// assertions let every page be checked even when an early one fails.
test("every page, at every width, in both themes", async ({ browser }) => {
  test.setTimeout(15 * 60_000);
  for (const viewport of viewports) {
    for (const theme of themes) {
      for (const target of targets) {
        // A fresh context per page, so one page's session or theme never
        // leaks into the next.
        const context = await browser.newContext();
        const page = await context.newPage();
        await test.step(`${target.name} at ${viewport.name}px, ${theme}`, () => audit(page, target, viewport, theme));
        await context.close();
      }
    }
  }
});
