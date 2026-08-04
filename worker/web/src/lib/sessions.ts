import type { Session } from "@/lib/api";

type SessionTitle = Pick<Session, "display_title" | "title" | "intent">;

export function displayTitle(session: SessionTitle): string {
  for (const value of [session.display_title, session.title, session.intent]) {
    if (value?.trim()) return value.trim();
  }
  return "Untitled session";
}
