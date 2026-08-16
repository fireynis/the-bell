import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { afterEach, describe, expect, it, vi } from "vitest";
import type { Post } from "../api/types";
import { AuthProvider } from "../context/AuthContext";
import PostCard from "./PostCard";

/**
 * What a post's image sounds like.
 *
 * Every image in the feed used to render as `alt=""` with no way for the author
 * to say otherwise, so a blind resident heard a post as text with a silent gap
 * in it. These pin the two places a description has to land — the card's image
 * and the control that opens it full size — and the empty case, which must stay
 * an empty alt rather than becoming an absent one.
 */

const answer = (body: unknown) =>
  Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve(body) } as unknown as Response);

function stubApi() {
  vi.stubGlobal("fetch", vi.fn(() => answer({})));
}

function post(overrides: Partial<Post> = {}): Post {
  return {
    id: "p1",
    author_id: "0193a7b2-aaaa-7000-8000-000000000001",
    author_display_name: "Ada",
    body: "Look at this",
    image_path: "/uploads/heron.jpg",
    alt_text: "",
    status: "visible",
    created_at: new Date().toISOString(),
    edited_at: null,
    ...overrides,
  };
}

function renderCard(p: Post) {
  stubApi();
  return render(
    <MemoryRouter>
      <AuthProvider>
        <PostCard post={p} />
      </AuthProvider>
    </MemoryRouter>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("PostCard image description", () => {
  it("names the image with the author's description on the control that opens it", () => {
    renderCard(post({ alt_text: "A heron on the frozen millpond" }));

    expect(
      screen.getByRole("button", {
        name: "View Ada's image full size: A heron on the frozen millpond",
      }),
    ).toBeInTheDocument();
  });

  it("falls back to naming the action when nobody described the image", () => {
    renderCard(post({ alt_text: "" }));

    expect(screen.getByRole("button", { name: "View Ada's image full size" })).toBeInTheDocument();
  });

  // The description is on the button, so an alt on the img inside it would have
  // a screen reader read the same sentence twice.
  it("leaves the thumbnail's alt empty so the description is announced once", () => {
    const { container } = renderCard(post({ alt_text: "A heron on the frozen millpond" }));

    const img = container.querySelector("article img[src='/uploads/heron.jpg']");
    expect(img).not.toBeNull();
    expect(img).toHaveAttribute("alt", "");
  });

  /**
   * An `alt` attribute that is absent rather than empty is the failure worth
   * guarding: a screen reader falls back to reading the filename, so an
   * undescribed image becomes "uploads heron dot jpg" instead of silence.
   *
   * A post served from a feed-cache entry written before alt_text existed
   * arrives with the field missing, which is exactly how `undefined` would
   * reach the attribute.
   */
  it("still renders an empty alt for a post that carries no alt_text at all", () => {
    const withoutField = post();
    delete (withoutField as { alt_text?: string }).alt_text;

    const { container } = renderCard(withoutField);

    const img = container.querySelector("article img[src='/uploads/heron.jpg']");
    expect(img).toHaveAttribute("alt", "");
  });

  it("carries the description into the lightbox, where the image is the content", async () => {
    renderCard(post({ alt_text: "A heron on the frozen millpond" }));

    fireEvent.click(
      screen.getByRole("button", {
        name: "View Ada's image full size: A heron on the frozen millpond",
      }),
    );

    await waitFor(() =>
      expect(screen.getByAltText("A heron on the frozen millpond")).toBeInTheDocument(),
    );
  });
});
