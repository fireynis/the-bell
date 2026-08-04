import { describe, expect, it } from "vitest";
import type { FeedResponse, Post } from "../api/types";
import { applyFeedPage, initialFeedState, mergeLivePosts } from "./feed";

function post(id: string, overrides: Partial<Post> = {}): Post {
  return {
    id,
    author_id: "author",
    body: "body",
    image_path: "",
    status: "visible",
    created_at: "2026-03-01T12:00:00Z",
    edited_at: null,
    ...overrides,
  };
}

function page(ids: string[], next_cursor?: string): FeedResponse {
  return { posts: ids.map((id) => post(id)), next_cursor };
}

describe("initialFeedState", () => {
  it("starts empty but willing to fetch, so the first page is requested", () => {
    expect(initialFeedState()).toEqual({ posts: [], cursor: undefined, hasMore: true });
  });

  it("hands out a fresh object each time, so a retry cannot reuse stale posts", () => {
    const first = initialFeedState();
    first.posts.push(post("a"));
    expect(initialFeedState().posts).toEqual([]);
  });
});

describe("applyFeedPage", () => {
  it("appends a page after the posts already loaded", () => {
    const state = applyFeedPage(initialFeedState(), page(["a", "b"], "c1"), true);
    const got = applyFeedPage(state, page(["c"], "c2"), true);
    expect(got.posts.map((p) => p.id)).toEqual(["a", "b", "c"]);
  });

  it("replaces the posts when not appending, so a retry starts clean", () => {
    const state = applyFeedPage(initialFeedState(), page(["a", "b"], "c1"), true);
    const got = applyFeedPage(state, page(["x"], undefined), false);
    expect(got.posts.map((p) => p.id)).toEqual(["x"]);
  });

  it("advances the cursor to the one the page carries", () => {
    const state = applyFeedPage(initialFeedState(), page(["a"], "cursor-1"), true);
    expect(state.cursor).toBe("cursor-1");
    expect(applyFeedPage(state, page(["b"], "cursor-2"), true).cursor).toBe("cursor-2");
  });

  // hasMore is derived rather than tracked so the feed can never ask for a page
  // it has no cursor for.
  it("derives hasMore from the presence of a next cursor", () => {
    expect(applyFeedPage(initialFeedState(), page(["a"], "c1"), true).hasMore).toBe(true);
    expect(applyFeedPage(initialFeedState(), page(["a"], undefined), true).hasMore).toBe(false);
    expect(applyFeedPage(initialFeedState(), page(["a"], ""), true).hasMore).toBe(false);
  });

  it("clears the cursor when the last page arrives", () => {
    const state = applyFeedPage(initialFeedState(), page(["a"], "c1"), true);
    expect(applyFeedPage(state, page(["b"]), true).cursor).toBeUndefined();
  });

  // Pages overlap when a post is created between two cursor fetches; rendering
  // the same post twice makes React warn about duplicate keys.
  it("drops posts an earlier page already delivered", () => {
    const state = applyFeedPage(initialFeedState(), page(["a", "b"], "c1"), true);
    const got = applyFeedPage(state, page(["b", "c"], "c2"), true);
    expect(got.posts.map((p) => p.id)).toEqual(["a", "b", "c"]);
  });

  it("keeps the posts and ends the feed when a page arrives empty", () => {
    const state = applyFeedPage(initialFeedState(), page(["a"], "c1"), true);
    const got = applyFeedPage(state, page([]), true);
    expect(got.posts.map((p) => p.id)).toEqual(["a"]);
    expect(got.hasMore).toBe(false);
  });

  it("treats a response with no posts field as an empty page", () => {
    const got = applyFeedPage(initialFeedState(), {} as FeedResponse, true);
    expect(got).toEqual({ posts: [], cursor: undefined, hasMore: false });
  });

  it("does not mutate the state it was given", () => {
    const state = applyFeedPage(initialFeedState(), page(["a"], "c1"), true);
    applyFeedPage(state, page(["b"], "c2"), true);
    expect(state.posts.map((p) => p.id)).toEqual(["a"]);
  });
});

describe("mergeLivePosts", () => {
  it("puts live arrivals above the loaded feed", () => {
    const got = mergeLivePosts([post("live")], [post("old")]);
    expect(got.map((p) => p.id)).toEqual(["live", "old"]);
  });

  // The same post reaches the reader twice: once over SSE and once in the first
  // page of a later fetch.
  it("shows a post once when it arrives both live and by page", () => {
    const got = mergeLivePosts([post("dup")], [post("dup"), post("old")]);
    expect(got.map((p) => p.id)).toEqual(["dup", "old"]);
  });

  it("keeps the live copy's position rather than the loaded one's", () => {
    const got = mergeLivePosts([post("b")], [post("a"), post("b")]);
    expect(got.map((p) => p.id)).toEqual(["b", "a"]);
  });

  it("deduplicates within the live batch itself", () => {
    const got = mergeLivePosts([post("x"), post("x")], []);
    expect(got.map((p) => p.id)).toEqual(["x"]);
  });

  it("returns the loaded feed unchanged when nothing arrived live", () => {
    expect(mergeLivePosts([], [post("a"), post("b")]).map((p) => p.id)).toEqual(["a", "b"]);
  });

  it("skips malformed entries rather than rendering a post with no key", () => {
    const got = mergeLivePosts([null as unknown as Post], [post("a")]);
    expect(got.map((p) => p.id)).toEqual(["a"]);
  });
});
