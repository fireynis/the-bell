import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthProvider } from "../context/AuthContext";
import Compose from "./Compose";

/**
 * The composer's image-description field.
 *
 * It exists because nothing ever asked: an author who attached a photo had no
 * way to say what was in it, so every image in the town's feed reached a screen
 * reader as silence. The field is deliberately not a gate — a post with an
 * undescribed image still goes up — so what these pin is that it appears with
 * the image, that it travels with the upload, and that it never blocks posting.
 */

const VIEWER_ID = "0193a7b2-aaaa-7000-8000-000000000001";

const answer = (body: unknown) =>
  Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) } as unknown as Response);

/** Stubs the session and profile reads, and records what a submit sends. */
function stubApi() {
  const posted: FormData[] = [];

  vi.stubGlobal(
    "fetch",
    vi.fn((url: string, init?: RequestInit) => {
      if (url.startsWith("/.ory")) return answer({ id: "session-1", active: true });
      if (url.startsWith("/api/v1/posts")) {
        if (init?.body instanceof FormData) posted.push(init.body);
        return answer({ id: "p1" });
      }
      return answer({
        id: VIEWER_ID,
        display_name: "Ada",
        bio: "",
        avatar_url: "",
        trust_score: 80,
        role: "member",
        is_active: true,
        joined_at: "2026-01-01T00:00:00Z",
      });
    }),
  );

  return posted;
}

/**
 * Renders the composer and waits for the member's profile to arrive.
 *
 * Until it does, postingBlockReason has no actor to check and the whole form is
 * disabled behind "You cannot post yet" — which is correct behaviour and not
 * what any of these tests are about.
 */
async function renderCompose() {
  const posted = stubApi();
  const { container } = render(
    <MemoryRouter>
      <AuthProvider>
        <Compose />
      </AuthProvider>
    </MemoryRouter>,
  );

  await screen.findByPlaceholderText("What's happening in town?");
  return { posted, container };
}

/** Attaches a PNG to the hidden file input, as the attach button does. */
function attachImage(container: HTMLElement) {
  const input = container.querySelector("input[type='file']") as HTMLInputElement;
  const file = new File(["fake-png-bytes"], "heron.png", { type: "image/png" });
  fireEvent.change(input, { target: { files: [file] } });
  return file;
}

/** Types a body into the composer, which every submit needs. */
function writeBody(text: string) {
  fireEvent.change(screen.getByPlaceholderText("What's happening in town?"), {
    target: { value: text },
  });
}

const ringTheBell = () => screen.getByRole("button", { name: /ring the bell/i });

const altField = () => screen.getByLabelText("Describe this image");

beforeEach(() => {
  // jsdom has no object URLs, and the preview is what the field hangs off.
  vi.stubGlobal("URL", {
    ...URL,
    createObjectURL: vi.fn(() => "blob:preview"),
    revokeObjectURL: vi.fn(),
  });
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("Compose image description", () => {
  it("asks for a description only once there is an image to describe", async () => {
    const { container } = await renderCompose();

    expect(screen.queryByLabelText("Describe this image")).toBeNull();

    attachImage(container);

    expect(altField()).toBeInTheDocument();
    expect(screen.getByText("For neighbours using screen readers.")).toBeInTheDocument();
  });

  it("sends the description alongside the image", async () => {
    const { posted, container } = await renderCompose();

    attachImage(container);
    writeBody("Look at this");
    fireEvent.change(altField(), { target: { value: "  A heron on the frozen millpond  " } });
    fireEvent.click(ringTheBell());

    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0].get("alt_text")).toBe("A heron on the frozen millpond");
    expect(posted[0].get("body")).toBe("Look at this");
  });

  // Posting must stay frictionless: the field is encouragement, not a gate.
  it("posts an undescribed image without complaint", async () => {
    const { posted, container } = await renderCompose();

    attachImage(container);
    writeBody("Look at this");
    fireEvent.click(ringTheBell());

    await waitFor(() => expect(posted).toHaveLength(1));
    expect(posted[0].get("alt_text")).toBe("");
  });

  it("disables posting while the description is over the limit", async () => {
    const { container } = await renderCompose();

    attachImage(container);
    writeBody("Look at this");
    fireEvent.change(altField(), { target: { value: "a".repeat(501) } });

    await waitFor(() => expect(ringTheBell()).toBeDisabled());
  });

  // A description of a picture that is no longer attached is a guaranteed 400,
  // so removing the image takes its description with it.
  it("drops the description when the image is removed", async () => {
    const { container } = await renderCompose();

    attachImage(container);
    fireEvent.change(altField(), { target: { value: "A heron on the frozen millpond" } });

    fireEvent.click(screen.getByRole("button", { name: "Remove image" }));

    expect(screen.queryByLabelText("Describe this image")).toBeNull();
  });
});
