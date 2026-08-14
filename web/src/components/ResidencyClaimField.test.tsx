import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import ResidencyClaimField from "./ResidencyClaimField";
import { AuthProvider } from "../context/AuthContext";
import { RESIDENCY_PROMPT } from "../lib/gating";
import { MAX_RESIDENCY_CLAIM_LENGTH } from "../lib/residency";

/**
 * The save answers 204 and nothing comes back, so the only evidence a member has
 * that they were heard is what this renders. These pin that evidence, and pin
 * that a failed save does not throw away the words they typed.
 */

interface StubOptions {
  /** What the self view carries, if this build's server sends the claim back. */
  claim?: string;
  /** Answers the save with a failure instead of 204. */
  failSave?: { status: number; error: string };
}

function stubApi(options: StubOptions = {}) {
  const saves: string[] = [];

  const answer = (body: unknown, ok = true, status = 200) =>
    Promise.resolve({ ok, status, json: () => Promise.resolve(body) } as unknown as Response);

  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, init?: RequestInit) => {
      if (url.startsWith("/.ory")) return answer({ id: "session-1", active: true });
      if (url.includes("/users/me/residency-claim")) {
        saves.push(String(init?.body ?? ""));
        if (options.failSave) {
          return answer({ error: options.failSave.error }, false, options.failSave.status);
        }
        return answer(undefined, true, 204);
      }
      if (url.startsWith("/api/v1/me")) {
        return answer({
          id: "viewer-1",
          display_name: "Newcomer",
          bio: "",
          avatar_url: "",
          trust_score: 0,
          role: "pending",
          is_active: true,
          joined_at: "2026-01-01T00:00:00Z",
          ...(options.claim === undefined ? {} : { residency_claim: options.claim }),
        });
      }
      return answer({});
    }),
  );

  return saves;
}

function renderField() {
  return render(
    <AuthProvider>
      <ResidencyClaimField />
    </AuthProvider>,
  );
}

const field = () => screen.getByLabelText(RESIDENCY_PROMPT.label);
const saveButton = () => screen.getByRole("button", { name: RESIDENCY_PROMPT.save });

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("ResidencyClaimField", () => {
  it("asks where in town they are, and says who will see the answer", () => {
    stubApi();
    renderField();

    expect(field()).toBeInTheDocument();
    expect(screen.getByText(RESIDENCY_PROMPT.privacy)).toBeInTheDocument();
  });

  it("sends the trimmed claim and acknowledges the save", async () => {
    const saves = stubApi();
    renderField();

    fireEvent.change(field(), { target: { value: "  By the old mill  " } });
    fireEvent.click(saveButton());

    await waitFor(() => expect(saves).toEqual([JSON.stringify({ claim: "By the old mill" })]));
    expect(await screen.findByText(RESIDENCY_PROMPT.saved)).toBeInTheDocument();
  });

  // A member who has already answered should see what they said, not a blank
  // box that invites them to type it again.
  it("starts from the claim already on the profile", async () => {
    stubApi({ claim: "Mill Lane" });
    renderField();

    await waitFor(() => expect(field()).toHaveValue("Mill Lane"));
  });

  it("starts empty when the profile carries no claim", () => {
    stubApi();
    renderField();

    expect(field()).toHaveValue("");
  });

  // Nothing to send, and a button that stays live would invite a member to keep
  // confirming what they have already said.
  it("offers no save until something has been typed", () => {
    stubApi();
    renderField();

    expect(saveButton()).toBeDisabled();
  });

  it("sends the empty string when a member clears what they said", async () => {
    const saves = stubApi({ claim: "Mill Lane" });
    renderField();
    await waitFor(() => expect(field()).toHaveValue("Mill Lane"));

    fireEvent.change(field(), { target: { value: "" } });
    fireEvent.click(saveButton());

    await waitFor(() => expect(saves).toEqual([JSON.stringify({ claim: "" })]));
  });

  it("refuses to send a claim past the limit, and says so", () => {
    stubApi();
    renderField();

    fireEvent.change(field(), {
      target: { value: "a".repeat(MAX_RESIDENCY_CLAIM_LENGTH + 1) },
    });

    expect(saveButton()).toBeDisabled();
    expect(screen.getByText(/the most you can say here is/i)).toBeInTheDocument();
  });

  // Retyping an address to find out whether the second attempt works is exactly
  // the treatment a newcomer does not need.
  it("keeps the typed words when the save fails, and says what happened", async () => {
    stubApi({ failSave: { status: 500, error: "internal error" } });
    renderField();

    fireEvent.change(field(), { target: { value: "By the old mill" } });
    fireEvent.click(saveButton());

    expect(await screen.findByText(/could not be saved/i)).toBeInTheDocument();
    expect(field()).toHaveValue("By the old mill");
    expect(screen.queryByText(RESIDENCY_PROMPT.saved)).not.toBeInTheDocument();
  });

  it("withdraws the acknowledgement once the answer changes again", async () => {
    stubApi();
    renderField();

    fireEvent.change(field(), { target: { value: "By the old mill" } });
    fireEvent.click(saveButton());
    expect(await screen.findByText(RESIDENCY_PROMPT.saved)).toBeInTheDocument();

    fireEvent.change(field(), { target: { value: "By the school" } });

    expect(screen.queryByText(RESIDENCY_PROMPT.saved)).not.toBeInTheDocument();
  });
});
