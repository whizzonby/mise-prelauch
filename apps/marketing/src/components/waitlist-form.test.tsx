import { ApiError } from "@mise/api-client";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { WaitlistForm } from "./waitlist-form";

const push = vi.fn();
const createLead = vi.fn();

vi.mock("next/navigation", () => ({ useRouter: () => ({ push }) }));
vi.mock("@/lib/api", () => ({ api: { createLead: (input: unknown) => createLead(input) } }));

function renderForm(props: Partial<Parameters<typeof WaitlistForm>[0]> = {}) {
  const client = new QueryClient({ defaultOptions: { mutations: { retry: false } } });
  return render(
    <QueryClientProvider client={client}>
      <WaitlistForm placement="test" {...props} />
    </QueryClientProvider>,
  );
}

async function fillRequired(user: ReturnType<typeof userEvent.setup>) {
  await user.type(screen.getByLabelText("First name"), "Asha");
  await user.type(screen.getByLabelText("Email"), "asha@example.com");
  await user.selectOptions(screen.getByLabelText("Where would you like delivery?"), "port-of-spain");
  await user.click(screen.getByRole("checkbox", { name: /Email me about the Mise launch/ }));
}

beforeEach(() => {
  push.mockReset();
  createLead.mockReset();
  window.localStorage.clear();
});

describe("WaitlistForm", () => {
  it("does not pre-tick the consent box", () => {
    renderForm();
    expect(screen.getByRole("checkbox", { name: /Email me about the Mise launch/ })).not.toBeChecked();
  });

  it("explains what is missing and does not call the API", async () => {
    const user = userEvent.setup();
    renderForm();
    await user.click(screen.getByRole("button", { name: "Join the waitlist" }));

    expect(await screen.findByText("Enter your first name.")).toBeInTheDocument();
    expect(screen.getByText("Enter your email address.")).toBeInTheDocument();
    expect(screen.getByText("Choose where you would like delivery.")).toBeInTheDocument();
    expect(screen.getByText("Tick the box so we can email you about the launch.")).toBeInTheDocument();
    expect(screen.getByLabelText("First name")).toHaveAttribute("aria-invalid", "true");
    expect(createLead).not.toHaveBeenCalled();
  });

  it("rejects a malformed email before submitting", async () => {
    const user = userEvent.setup();
    renderForm();
    await fillRequired(user);
    await user.clear(screen.getByLabelText("Email"));
    await user.type(screen.getByLabelText("Email"), "asha@");
    await user.click(screen.getByRole("button", { name: "Join the waitlist" }));

    expect(await screen.findByText("Enter a valid email address, like name@example.com.")).toBeInTheDocument();
    expect(createLead).not.toHaveBeenCalled();
  });

  it("sends the signup, keeps the profile token and goes to the welcome page", async () => {
    createLead.mockResolvedValue({ outcome: "created", profile_token: "tok.en", lead: {} });
    const user = userEvent.setup();
    renderForm({ referralCode: "ABC2345" });
    await fillRequired(user);
    await user.click(screen.getByLabelText("Vegetarian"));
    await user.selectOptions(screen.getByLabelText(/Household size/), "3");
    await user.click(screen.getByRole("button", { name: "Join the waitlist" }));

    await waitFor(() => expect(push).toHaveBeenCalledWith("/welcome"));
    expect(createLead).toHaveBeenCalledTimes(1);
    expect(createLead.mock.calls[0]![0]).toMatchObject({
      first_name: "Asha",
      email: "asha@example.com",
      location: "port-of-spain",
      dietary_interests: ["vegetarian"],
      household_size: 3,
      consent: true,
      referral_code: "ABC2345",
      website: "",
    });
    expect(window.localStorage.getItem("mise.profile_token")).toBe("tok.en");
  });

  it("tells someone who is already on the list, without navigating", async () => {
    createLead.mockResolvedValue({ outcome: "already_on_list" });
    const user = userEvent.setup();
    renderForm();
    await fillRequired(user);
    await user.click(screen.getByRole("button", { name: "Join the waitlist" }));

    expect(await screen.findByText("You are already on the list")).toBeInTheDocument();
    expect(push).not.toHaveBeenCalled();
  });

  it("shows the API's field errors on the right fields", async () => {
    createLead.mockRejectedValue(
      new ApiError(422, "validation_failed", "Some fields need attention.", [
        { field: "email", code: "invalid", message: "Enter a valid email address, like name@example.com." },
      ]),
    );
    const user = userEvent.setup();
    renderForm();
    await fillRequired(user);
    await user.click(screen.getByRole("button", { name: "Join the waitlist" }));

    expect(await screen.findByText("Enter a valid email address, like name@example.com.")).toBeInTheDocument();
    expect(screen.getByLabelText("Email")).toHaveAttribute("aria-invalid", "true");
  });

  it("explains a network failure and lets the person try again", async () => {
    createLead.mockRejectedValue(new Error("offline"));
    const user = userEvent.setup();
    renderForm();
    await fillRequired(user);
    await user.click(screen.getByRole("button", { name: "Join the waitlist" }));

    expect(await screen.findByText("We could not reach Mise. Check your connection and try again.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Join the waitlist" })).toBeEnabled();
  });
});
