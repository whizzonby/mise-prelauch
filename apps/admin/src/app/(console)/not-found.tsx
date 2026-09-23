import { Heading, Text } from "@mise/ui";
import Link from "next/link";

export default function NotFound() {
  return (
    <>
      <Heading as="h1" size="h2">
        Not found
      </Heading>
      <Text className="mt-3">This lead does not exist. It may have been deleted.</Text>
      <Link href="/leads" className="mt-4 inline-block font-semibold underline underline-offset-4">
        Back to all leads
      </Link>
    </>
  );
}
