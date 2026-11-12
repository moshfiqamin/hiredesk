import { redirect } from "next/navigation";

// The open roles are the front door: the one page a stranger can use without
// an account. Signed-in users reach their own home from the header.
export default function Home() {
  redirect("/roles");
}
