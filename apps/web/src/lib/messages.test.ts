import { ApiError, NetworkError } from "@mise/api-client";
import { describe, expect, it } from "vitest";

import { formError } from "./messages";

describe("formError", () => {
  it("maps field errors to their fields", () => {
    const state = formError(
      new ApiError(422, "validation_failed", "Some fields need attention.", [
        { field: "password", code: "weak", message: "Use at least 12 characters." },
      ]),
    );
    expect(state.fields).toEqual({ password: "Use at least 12 characters." });
  });

  it("never reveals whether an account exists", () => {
    // A wrong password and an unknown address read the same.
    expect(formError(new ApiError(401, "invalid_credentials", "x")).error).toBe("Email or password is incorrect.");
  });

  it("explains closed registration and network failures", () => {
    expect(formError(new ApiError(403, "registration_closed", "x")).error).toMatch(/not open yet/);
    expect(formError(new NetworkError()).error).toMatch(/could not be reached/);
  });
});
