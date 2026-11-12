// Prints the WCAG contrast ratio of every text-on-surface pairing the design
// tokens are used in, for the light and the dark theme, read straight from
// src/app/globals.css so the report can never drift from the CSS.
//
//   node scripts/contrast-report.mjs
//
// AA asks for 4.5:1 for body text and 3:1 for large text and for UI
// component boundaries (WCAG 1.4.3 and 1.4.11). The script exits 1 if any
// text pair is under 4.5:1 or any boundary pair under 3:1.

import { readFileSync } from "node:fs";

const css = readFileSync(new URL("../src/app/globals.css", import.meta.url), "utf8");

/** block returns the custom properties declared in the first rule whose selector is exactly `selector`. */
function block(selector) {
  const start = css.indexOf(`\n${selector} {`);
  const body = css.slice(start, css.indexOf("\n}", start));
  return Object.fromEntries([...body.matchAll(/--([\w-]+):\s*([^;]+);/g)].map((m) => [m[1], m[2].trim()]));
}

// Two :root blocks (palette, then light tokens); later declarations win, as in CSS.
const rootStart = css.indexOf("\n:root {");
const secondRoot = css.indexOf("\n:root {", rootStart + 1);
const light = { ...block(":root"), ...Object.fromEntries([...css.slice(secondRoot, css.indexOf("\n}", secondRoot)).matchAll(/--([\w-]+):\s*([^;]+);/g)].map((m) => [m[1], m[2].trim()])) };
const dark = { ...light, ...block(".dark") };

/** resolve follows var(--x) references down to a hex colour. */
function resolve(tokens, name, depth = 0) {
  const value = tokens[name];
  if (!value || depth > 5) throw new Error(`cannot resolve --${name}`);
  const ref = value.match(/^var\(--([\w-]+)\)$/);
  return ref ? resolve(tokens, ref[1], depth + 1) : value;
}

/** luminance is WCAG relative luminance of a #rrggbb colour. */
function luminance(hex) {
  const [r, g, b] = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255);
  const lin = (c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

const ratio = (a, b) => {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
};

// [foreground, background, minimum, what it is]
const pairs = [
  ["foreground", "background", 4.5, "body text"],
  ["foreground", "surface", 4.5, "text on cards"],
  ["text-secondary", "background", 4.5, "secondary text"],
  ["text-secondary", "surface", 4.5, "secondary text on cards"],
  ["text-secondary", "sidebar", 4.5, "sidebar links and email"],
  ["muted-foreground", "background", 4.5, "muted text"],
  ["muted-foreground", "surface", 4.5, "muted text on cards"],
  ["muted-foreground", "surface-sunken", 4.5, "muted text on sunken panels"],
  ["primary-foreground", "primary", 4.5, "primary button label"],
  ["primary-ink", "primary-soft", 4.5, "selected chip and highlight"],
  ["primary-ink", "surface", 4.5, "primary-coloured text"],
  ["band-strong", "band-strong-bg", 4.5, "strong band chip"],
  ["band-adequate", "band-adequate-bg", 4.5, "adequate band chip"],
  ["band-weak", "band-weak-bg", 4.5, "weak band chip"],
  ["band-weak", "surface", 4.5, "zero-score explanation"],
  ["destructive", "surface", 4.5, "error text"],
  ["border-field", "surface", 3, "input border (non-text)"],
  ["ring", "background", 3, "focus ring (non-text)"],
];

let failed = false;
for (const [theme, tokens] of [["light", light], ["dark", dark]]) {
  console.log(`\n${theme}`);
  for (const [fg, bg, min, what] of pairs) {
    const r = ratio(resolve(tokens, fg), resolve(tokens, bg));
    const ok = r >= min;
    if (!ok) failed = true;
    console.log(`  ${ok ? "pass" : "FAIL"}  ${r.toFixed(2).padStart(5)}:1  (min ${min})  ${fg} on ${bg}  — ${what}`);
  }
}
process.exit(failed ? 1 : 0);
