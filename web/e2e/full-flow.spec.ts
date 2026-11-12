import { expect, test, type Browser, type Page } from "@playwright/test";

import { seeded } from "./helpers";

// The whole product, end to end, through the real UI against the real stack:
//
//   admin     signs in, creates a role, writes its questions and rubric, opens it
//   candidate finds it on the public board, registers, applies
//   admin     sees the score and the ranking, assigns an interviewer
//   interviewer signs in, finds the candidate first in their worklist, reviews
//   admin     sees the review arrive
//
// Labels are matched exactly: this version of Next.js keeps the previous
// page in the DOM, hidden, so a substring like "Title" would also match the
// roles list's "Search by title" box still sitting behind the form.
//
// Every locator is a role, a label or visible text: what a person (or a
// screen reader) uses to find a control. None is a CSS class or test ID, so
// restyling cannot break the test, and a test that cannot find "Sign in" by
// its label has found a real accessibility bug.
//
// Needs `make up && make seed`. Makes four auth requests, under the API's
// limit of ten a minute per address.

// One run's names, unique so the test can run again on the same database.
const run = Date.now();
const roleTitle = `E2E Go Engineer ${run}`;
const candidate = { name: `E2E Candidate ${run}`, email: `e2e-${run}@hiredesk.test`, password: "e2e-candidate-password" };

test.describe.configure({ mode: "serial" });

/** newPersona opens a separate browser context: its own storage, so three people can be signed in at once. */
async function newPersona(browser: Browser): Promise<Page> {
  const context = await browser.newContext();
  return context.newPage();
}

/** signIn uses the real login form, as a person would. */
async function signIn(page: Page, email: string, password: string) {
  await page.goto("/login");
  await page.getByLabel("Email", { exact: true }).fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
}

let admin: Page;
let roleUrl = "";

test("admin creates a role with questions and a rubric, and opens it", async ({ browser }) => {
  admin = await newPersona(browser);
  await signIn(admin, seeded.admin.email, seeded.admin.password);
  await expect(admin).toHaveURL(/\/admin\/roles$/);

  // The role itself.
  await admin.getByRole("link", { name: "Create role" }).first().click();
  await admin.getByLabel("Title", { exact: true }).fill(roleTitle);
  await admin.getByLabel("Location", { exact: true }).fill("Remote");
  await admin.getByLabel("Vacancies", { exact: true }).fill("2");
  await admin.getByRole("button", { name: "Create draft" }).click();
  await expect(admin.getByRole("heading", { level: 1, name: roleTitle })).toBeVisible();
  roleUrl = admin.url();

  // Two questions: a written one the keywords are read from, and a number.
  const sections = admin.getByRole("navigation", { name: "Role sections" });
  await sections.getByRole("link", { name: "Questions" }).click();
  const questions = admin.getByRole("list", { name: "Questions" });
  const addQuestion = admin.getByRole("button", { name: "Add question" });

  await addQuestion.click();
  const first = questions.getByRole("listitem").nth(0);
  await first.getByLabel("Question", { exact: true }).fill("Describe a Go project you built.");
  await first.getByLabel("Answer type", { exact: true }).selectOption({ label: "Text" });
  await first.getByLabel("Minimum length", { exact: true }).fill("20");

  await addQuestion.click();
  const second = questions.getByRole("listitem").nth(1);
  await second.getByLabel("Question", { exact: true }).fill("Years of Go?");
  await second.getByLabel("Answer type", { exact: true }).selectOption({ label: "Number" });

  await admin.getByRole("button", { name: "Save form" }).click();
  await expect(admin.getByRole("button", { name: "Save form" })).toBeDisabled();

  // The rubric: two keyword criteria on the project answer, 60 and 40 points.
  await sections.getByRole("link", { name: "Rubric" }).click();
  const criteria = admin.getByRole("list", { name: "Criteria" });
  // A criterion card is the list item that has a Weight field; the keyword
  // chips inside a card are list items too, so this tells them apart.
  const card = (i: number) => criteria.getByRole("listitem").filter({ has: admin.getByLabel("Weight", { exact: true }) }).nth(i);

  // Both criteria keep the default kind, keyword match. Switching a
  // criterion to another kind and then editing it can empty its config and
  // crash the card: a defect in the rubric builder (module 11.6) described,
  // with how to reproduce and fix it, in docs/notes/14-3 section 9. Once it
  // is fixed, make the second criterion "Experience in years" on question 2.
  const keywordCriterion = async (i: number, name: string, weight: string, keywords: string[]) => {
    await admin.getByRole("button", { name: "Add criterion" }).click();
    await expect(card(i).getByRole("radio", { name: /Keyword match/ })).toBeChecked();
    await card(i).getByLabel("Name", { exact: true }).fill(name);
    await card(i).getByLabel("Weight", { exact: true }).fill(weight);
    await card(i).getByLabel("Question it reads", { exact: true }).selectOption({ label: "1. Describe a Go project you built." });
    for (const keyword of keywords) {
      await card(i).getByRole("textbox", { name: "Keywords" }).fill(keyword);
      await card(i).getByRole("textbox", { name: "Keywords" }).press("Enter");
    }
  };
  await keywordCriterion(0, "Go knowledge", "60", ["go", "goroutines"]);
  await keywordCriterion(1, "Data stores", "40", ["postgres", "kafka"]);

  await admin.getByRole("button", { name: "Save rubric" }).click();
  await expect(admin.getByRole("button", { name: "Save rubric" })).toBeDisabled();

  // Open it: the button only works once the form and rubric are complete.
  await admin.getByRole("button", { name: "Open for applications" }).click();
  await expect(admin.getByRole("button", { name: "Close role" })).toBeVisible();
});

test("a candidate finds the role, registers and applies", async ({ browser }) => {
  const page = await newPersona(browser);

  await page.goto("/roles");
  await page.getByRole("link", { name: roleTitle }).click();
  await expect(page.getByRole("heading", { level: 2, name: "What you will be asked" })).toBeVisible();
  await page.getByRole("link", { name: "Create an account to apply" }).click();

  await page.getByLabel("Full name", { exact: true }).fill(candidate.name);
  await page.getByLabel("Email", { exact: true }).fill(candidate.email);
  await page.getByLabel("Password", { exact: true }).fill(candidate.password);
  await page.getByRole("button", { name: "Create account" }).click();

  // Registration signs in and returns to the form for this role.
  await expect(page.getByRole("heading", { level: 1, name: `Apply for ${roleTitle}` })).toBeVisible();
  await page.getByRole("textbox", { name: "Link to your CV" }).fill("https://example.com/e2e-cv.pdf");
  await page.getByLabel(/Describe a Go project you built/).fill("I built a Go service with goroutines and Postgres for job queues.");
  await page.getByLabel(/Years of Go/).fill("4");
  await page.getByRole("button", { name: "Send application" }).click();

  await expect(page).toHaveURL(/\/my-applications$/);
  const application = page.getByRole("article").filter({ hasText: roleTitle });
  await expect(application).toBeVisible();
  // The tracker says, in text, which step the application is at.
  await expect(application.getByRole("listitem").filter({ hasText: "Received" })).toHaveAttribute("aria-current", "step");
});

test("the admin sees the score and the ranking, and assigns an interviewer", async () => {
  await admin.goto(roleUrl);
  await admin.getByRole("navigation", { name: "Role sections" }).getByRole("link", { name: "Ranking" }).click();

  const row = admin.getByRole("link", { name: new RegExp(candidate.name) });
  await expect(row).toBeVisible();
  await row.click();

  await expect(admin.getByRole("heading", { level: 1, name: candidate.name })).toBeVisible();
  const score = admin.getByRole("region", { name: "Score and evidence" });
  // Go knowledge 2 of 2 keywords (60) plus Data stores 1 of 2 (20): 80 of 100.
  await expect(score.getByText("80.0%")).toBeVisible();
  await expect(score.getByText("matched 2 of 2 keywords: go, goroutines")).toBeVisible();
  // The partial criterion shows its evidence with the matched word marked.
  await expect(score.getByText("matched 1 of 2 keywords: postgres")).toBeVisible();

  await admin.getByRole("button", { name: "Manage" }).click();
  const dialog = admin.getByRole("dialog", { name: `Interviewers for ${candidate.name}` });
  await dialog.getByRole("button", { name: `Assign ${seeded.interviewer.name}` }).click();
  await expect(dialog.getByRole("button", { name: `Remove ${seeded.interviewer.name}` })).toBeVisible();
  await admin.keyboard.press("Escape");
  await expect(admin.getByRole("region", { name: "Interviewers" }).getByText(seeded.interviewer.name)).toBeVisible();
});

test("the interviewer reviews the candidate, and the admin sees it", async ({ browser }) => {
  const page = await newPersona(browser);
  await signIn(page, seeded.interviewer.email, seeded.interviewer.password);
  await expect(page).toHaveURL(/\/assignments$/);

  await page.getByRole("link", { name: `Review ${candidate.name}` }).click();
  await expect(page.getByRole("heading", { level: 1, name: candidate.name })).toBeVisible();

  const form = page.getByRole("form", { name: "Your review" });
  await form.getByRole("radio", { name: "8, Lean yes, without reservation" }).check();
  await form.getByLabel("Notes", { exact: true }).fill("Clear on goroutines and queue design.");
  await form.getByRole("button", { name: "Submit review" }).click();

  // The form turns into the interviewer's own review, with Edit.
  const mine = page.getByRole("region", { name: "Your review" });
  await expect(mine.getByText("Lean yes")).toBeVisible();
  await expect(mine.getByRole("button", { name: "Edit" })).toBeVisible();

  await admin.reload();
  await expect(admin.getByRole("region", { name: "Interview reviews" }).getByText("8.0")).toBeVisible();
});
