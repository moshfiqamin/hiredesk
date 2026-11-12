import type { Metadata } from "next";

import { RolesBoard } from "@/components/listings/roles-board";

export const metadata: Metadata = { title: "Open roles" };

export default function RolesPage() {
  return <RolesBoard />;
}
