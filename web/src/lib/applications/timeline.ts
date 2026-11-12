import type { ApplicationEvent } from "@/lib/api/types";

const statusWords: Record<string, string> = {
  submitted: "submitted",
  shortlisted: "the shortlist",
  interview: "interview",
  offer: "offer",
  rejected: "rejected",
};

const num = (v: unknown) => (typeof v === "number" ? (Number.isInteger(v) ? String(v) : v.toFixed(1)) : "?");

/** TimelineLine is one event told as a sentence, with a short kind for its icon. */
export interface TimelineLine {
  sentence: string;
  kind: "applied" | "status" | "rejected" | "score" | "people" | "review";
}

/**
 * describeEvent tells one event as a sentence: "Rafi assigned Nadia as
 * interviewer" rather than "application.assigned". names resolves user IDs
 * found in payloads (an interviewer) to names; an ID it cannot resolve
 * becomes "an interviewer" rather than a UUID.
 */
export function describeEvent(e: ApplicationEvent, names: Map<string, string>, candidate: string): TimelineLine {
  const actor = e.actor_name ?? "The system";
  const p = e.payload;
  const who = (id: unknown) => (typeof id === "string" && names.get(id)) || "an interviewer";

  switch (e.type) {
    case "application.submitted":
      return { kind: "applied", sentence: `${candidate} applied, answering ${num(p.answers)} questions` };
    case "application.status_changed": {
      const to = String(p.to);
      if (to === "rejected") return { kind: "rejected", sentence: `${actor} rejected the application` };
      if (to === "shortlisted") return { kind: "status", sentence: `${actor} shortlisted ${candidate}` };
      return { kind: "status", sentence: `${actor} moved ${candidate} to ${statusWords[to] ?? to}` };
    }
    case "application.rescored": {
      const before = p.old_total_earned;
      const after = `${num(p.new_total_earned)} of ${num(p.new_total_available)}`;
      const sentence =
        typeof before === "number"
          ? `${actor} rescored against rubric v${num(p.new_rubric_version)}: ${num(before)} → ${after} points`
          : `${actor} scored against rubric v${num(p.new_rubric_version)}: ${after} points`;
      return { kind: "score", sentence };
    }
    case "application.assigned":
      return { kind: "people", sentence: `${actor} assigned ${who(p.interviewer_id)} as interviewer` };
    case "application.unassigned":
      return { kind: "people", sentence: `${actor} removed ${who(p.interviewer_id)} from interviewing` };
    case "application.reviewed":
      return { kind: "review", sentence: `${actor} reviewed and gave ${num(p.mark)} out of 10` };
    case "application.review_updated":
      if (p.from_mark !== p.to_mark) {
        return { kind: "review", sentence: `${actor} changed their mark from ${num(p.from_mark)} to ${num(p.to_mark)}` };
      }
      return { kind: "review", sentence: `${actor} edited their review notes` };
    default:
      // A type added on the server before this client knows it: still readable.
      return { kind: "status", sentence: `${actor}: ${e.type.replace(/^application\./, "").replaceAll("_", " ")}` };
  }
}

const relative = new Intl.RelativeTimeFormat("en-GB", { numeric: "auto" });

/** relativeTime renders an ISO time as "3 hours ago" or "yesterday". */
export function relativeTime(iso: string, now = Date.now()): string {
  const seconds = Math.round((new Date(iso).getTime() - now) / 1000);
  const steps: [Intl.RelativeTimeFormatUnit, number][] = [
    ["year", 31536000],
    ["month", 2592000],
    ["week", 604800],
    ["day", 86400],
    ["hour", 3600],
    ["minute", 60],
  ];
  for (const [unit, size] of steps) {
    if (Math.abs(seconds) >= size) return relative.format(Math.round(seconds / size), unit);
  }
  return "just now";
}
