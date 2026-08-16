import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Post } from "../api/types";
import EditPostDialog from "./EditPostDialog";

/**
 * Editing a post's image description.
 *
 * The dialog is the second chance at the thing the composer asks for and an
 * author in a hurry skips, so the field has to be here — and the request it
 * sends has to distinguish "I did not touch the description" from "clear it".
 * Getting that wrong strips a description off an image every time somebody
 * fixes a typo, with nothing on screen to show it happened.
 */

function post(overrides: Partial<Post> = {}): Post {
  return {
    id: "p1",
    author_id: "author",
    body: "Look at this",
    image_path: "/uploads/heron.jpg",
    alt_text: "A heron on the frozen millpond",
    status: "visible",
    created_at: new Date().toISOString(),
    edited_at: null,
    ...overrides,
  };
}

/** Records the PATCH payloads the dialog sends. */
function stubPatch(p: Post) {
  const sent: Array<Record<string, unknown>> = [];

  vi.stubGlobal(
    "fetch",
    vi.fn((_url: string, init?: RequestInit) => {
      if (init?.body) sent.push(JSON.parse(init.body as string));
      return Promise.resolve({
        ok: true,
        status: 200,
        json: () => Promise.resolve(p),
      } as unknown as Response);
    }),
  );

  return sent;
}

const altField = () => screen.getByLabelText("Describe this image");
const saveButton = () => screen.getByRole("button", { name: "Save changes" });

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("EditPostDialog image description", () => {
  it("offers the description of a post that has an image, prefilled", () => {
    render(<EditPostDialog post={post()} onClose={() => {}} onSaved={() => {}} />);

    expect(altField()).toHaveValue("A heron on the frozen millpond");
  });

  // A description on a post with no image is a 400 from the server, so offering
  // the field would be offering an edit that cannot be saved.
  it("offers no description field on a text-only post", () => {
    render(
      <EditPostDialog
        post={post({ image_path: "", alt_text: "" })}
        onClose={() => {}}
        onSaved={() => {}}
      />,
    );

    expect(screen.queryByLabelText("Describe this image")).toBeNull();
  });

  it("saves a changed description, leaving the body as it was", async () => {
    const p = post();
    const sent = stubPatch(p);
    render(<EditPostDialog post={p} onClose={() => {}} onSaved={() => {}} />);

    fireEvent.change(altField(), { target: { value: "A heron, wings half open" } });
    fireEvent.click(saveButton());

    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toEqual({ body: "Look at this", alt_text: "A heron, wings half open" });
  });

  // The contract this whole field turns on: an untouched description is left
  // out of the payload, which tells the server to keep what it has.
  it("omits the description entirely when only the body changed", async () => {
    const p = post();
    const sent = stubPatch(p);
    render(<EditPostDialog post={p} onClose={() => {}} onSaved={() => {}} />);

    fireEvent.change(screen.getByLabelText("Post"), { target: { value: "Look at this heron" } });
    fireEvent.click(saveButton());

    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toEqual({ body: "Look at this heron" });
    expect(sent[0]).not.toHaveProperty("alt_text");
  });

  // Emptying the field is the only way to say "describe nothing", so it has to
  // reach the server as a sent-and-empty value rather than as an omission.
  it("sends an empty description when the author clears it", async () => {
    const p = post();
    const sent = stubPatch(p);
    render(<EditPostDialog post={p} onClose={() => {}} onSaved={() => {}} />);

    fireEvent.change(altField(), { target: { value: "" } });
    fireEvent.click(saveButton());

    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0]).toEqual({ body: "Look at this", alt_text: "" });
  });

  // Saving is disabled until something changes; a description-only edit is a
  // change, and it used to be impossible to make.
  it("enables saving for a description-only edit", () => {
    render(<EditPostDialog post={post()} onClose={() => {}} onSaved={() => {}} />);

    expect(saveButton()).toBeDisabled();
    fireEvent.change(altField(), { target: { value: "A heron, wings half open" } });
    expect(saveButton()).toBeEnabled();
  });

  it("refuses to send a description past the server's limit", async () => {
    const p = post();
    const sent = stubPatch(p);
    render(<EditPostDialog post={p} onClose={() => {}} onSaved={() => {}} />);

    fireEvent.change(altField(), { target: { value: "a".repeat(501) } });
    fireEvent.click(saveButton());

    expect(saveButton()).toBeDisabled();
    await waitFor(() => expect(sent).toHaveLength(0));
  });
});
