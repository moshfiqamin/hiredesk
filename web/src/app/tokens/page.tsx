import type { Metadata } from "next";

import { ThemeToggle } from "@/components/theme-toggle";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

// A specimen of the design tokens, so every token can be checked in both
// themes. It was the home page until the first screens existed; it stays at
// /tokens as a reference while building new ones.

const neutrals = [
  { token: "--background", use: "Page" },
  { token: "--surface", use: "Raised surface" },
  { token: "--surface-sunken", use: "Wells, inputs at rest" },
  { token: "--sidebar", use: "Navigation" },
  { token: "--border", use: "Hairlines" },
  { token: "--muted-foreground", use: "Hints, metadata" },
  { token: "--text-secondary", use: "Supporting text" },
  { token: "--foreground", use: "Body text" },
] as const;

const typeScale = [
  { cls: "text-2xl font-semibold", label: "2xl · 30px · page title" },
  { cls: "text-xl font-semibold", label: "xl · 24px · section title" },
  { cls: "text-lg font-medium", label: "lg · 19px · panel heading" },
  { cls: "text-md", label: "md · 16px · long-form answers" },
  { cls: "text-base", label: "base · 14px · body and tables" },
  { cls: "text-sm", label: "sm · 13px · controls and labels" },
  { cls: "text-xs", label: "xs · 12px · metadata" },
] as const;

const applicants = [
  { name: "Amara Okafor", score: 88, band: "strong", stage: "Interview" },
  { name: "Mei-Lin Zhou", score: 76, band: "strong", stage: "Screening" },
  { name: "Priya Natarajan", score: 57, band: "adequate", stage: "Submitted" },
  { name: "Kofi Mensah", score: 41, band: "weak", stage: "Rejected" },
] as const;

export const metadata: Metadata = { title: "Design tokens" };

export default function TokensPage() {
  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-10 px-4 py-8 sm:px-6">
      <header className="flex items-center justify-between gap-4 border-b pb-4">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">HireDesk design tokens</h1>
          <p className="mt-1 text-text-secondary">
            Every colour, size and space the interface is allowed to use, in the current theme.
          </p>
        </div>
        <ThemeToggle />
      </header>

      <section aria-labelledby="neutrals" className="flex flex-col gap-3">
        <h2 id="neutrals" className="text-lg font-medium">Neutral scale</h2>
        <ul className="grid grid-cols-2 gap-3 sm:grid-cols-4">
          {neutrals.map((n) => (
            <li key={n.token} className="flex flex-col gap-1 text-sm">
              <span
                className="h-12 rounded-lg border"
                style={{ background: `var(${n.token})` }}
              />
              <code className="font-mono text-xs">{n.token}</code>
              <span className="text-muted-foreground">{n.use}</span>
            </li>
          ))}
        </ul>
      </section>

      <section aria-labelledby="actions" className="flex flex-col gap-3">
        <h2 id="actions" className="text-lg font-medium">Accent and actions</h2>
        <p className="max-w-prose text-text-secondary">
          Deep sage is the only accent. It marks the primary action, the current
          selection and keyboard focus. One primary button per view.
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <Button>Assign interviewer</Button>
          <Button variant="outline">Rescore</Button>
          <Button variant="ghost">Cancel</Button>
          <Button variant="destructive">Close role</Button>
          <Button disabled>Disabled</Button>
        </div>
        <div className="flex max-w-sm flex-col gap-1.5">
          <Label htmlFor="specimen-search">Search answers</Label>
          <Input id="specimen-search" placeholder="keyset pagination" />
        </div>
      </section>

      <section aria-labelledby="bands" className="flex flex-col gap-3">
        <h2 id="bands" className="text-lg font-medium">Score bands</h2>
        <p className="max-w-prose text-text-secondary">
          Colour, fill pattern and word together, so the band reads correctly
          with any form of colour blindness and in greyscale.
        </p>
        <div className="flex flex-wrap gap-3">
          <span className="score-band" data-band="strong">Strong · 75 and above</span>
          <span className="score-band" data-band="adequate">Adequate · 50 to 74</span>
          <span className="score-band" data-band="weak">Weak · below 50</span>
        </div>
        <div className="overflow-x-auto rounded-lg border bg-surface">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Candidate</TableHead>
                <TableHead>Score</TableHead>
                <TableHead>Stage</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {applicants.map((a) => (
                <TableRow key={a.name}>
                  <TableCell className="font-medium">{a.name}</TableCell>
                  <TableCell>
                    <span className="mr-2 font-mono text-sm">{a.score}</span>
                    <span className="score-band capitalize" data-band={a.band}>
                      {a.band}
                    </span>
                  </TableCell>
                  <TableCell className="text-text-secondary">{a.stage}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </section>

      <section aria-labelledby="type" className="flex flex-col gap-3">
        <h2 id="type" className="text-lg font-medium">Type scale</h2>
        <ul className="flex flex-col gap-2">
          {typeScale.map((t) => (
            <li key={t.label} className={t.cls}>
              {t.label}
            </li>
          ))}
        </ul>
      </section>

      <section aria-labelledby="space" className="flex flex-col gap-3">
        <h2 id="space" className="text-lg font-medium">Spacing scale</h2>
        <ul className="flex flex-col gap-2 text-sm">
          {[1, 2, 3, 4, 5, 6, 8, 10, 12].map((step) => (
            <li key={step} className="flex items-center gap-3">
              <code className="w-20 font-mono text-xs">--space-{step}</code>
              <span
                className="h-3 rounded-sm bg-primary"
                style={{ width: `var(--space-${step})` }}
              />
              <span className="text-muted-foreground">{step * 4}px</span>
            </li>
          ))}
        </ul>
      </section>
    </div>
  );
}
