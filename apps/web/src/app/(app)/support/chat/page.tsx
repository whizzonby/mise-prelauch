import { redirect } from "next/navigation";

/** Conversations live in the chat panel; this address opens it. */
export default function SupportChatPage() {
  redirect("/dashboard?chat=open");
}
