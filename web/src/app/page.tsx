import { redirect } from "next/navigation";

// The admin roles list is the front door. AdminShell sends anyone not signed
// in on to /login, with this destination remembered.
export default function Home() {
  redirect("/admin/roles");
}
