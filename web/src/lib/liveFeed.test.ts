import { describe, expect, it } from "vitest";
import type { Post } from "../api/types";
import {
  describeReactions,
  mergePendingPosts,
  parseEventData,
  reactionEmoji,
  summarizeReactions,
  type ReactionEvent,
} from "./liveFeed";

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

function reaction(post_id: string, reaction_type: string): ReactionEvent {
  return { post_id, reaction_type };
}

describe("summarizeReactions", () => {
  it("returns nothing for an empty buffer", () => {
    expect(summarizeReactions([])).toEqual([]);
  });

  it("reports a single reaction", () => {
    expect(summarizeReactions([reaction("p1", "bell")])).toEqual([
      { post_id: "p1", reaction_type: "bell", count: 1 },
    ]);
  });

  it("counts repeated reactions of the same type on the same post", () => {
    const events = [reaction("p1", "bell"), reaction("p1", "bell"), reaction("p1", "bell")];

    expect(summarizeReactions(events)).toEqual([
      { post_id: "p1", reaction_type: "bell", count: 3 },
    ]);
  });

  // The bug this function exists to prevent: counting the whole buffer while
  // labelling it with the first event attributed every reaction to one post.
  it("does not attribute reactions from other posts to the first one", () => {
    const events = [reaction("p1", "bell"), reaction("p2", "bell"), reaction("p3", "bell")];

    const got = summarizeReactions(events);

    expect(got).toHaveLength(3);
    expect(got.every((n) => n.count === 1)).toBe(true);
    expect(got.map((n) => n.post_id)).toEqual(["p1", "p2", "p3"]);
  });

  it("separates reaction types on the same post", () => {
    const events = [reaction("p1", "bell"), reaction("p1", "heart"), reaction("p1", "bell")];

    expect(summarizeReactions(events)).toEqual([
      { post_id: "p1", reaction_type: "bell", count: 2 },
      { post_id: "p1", reaction_type: "heart", count: 1 },
    ]);
  });

  it("preserves arrival order of the first event in each group", () => {
    const events = [reaction("p2", "heart"), reaction("p1", "bell"), reaction("p2", "heart")];

    expect(summarizeReactions(events).map((n) => n.post_id)).toEqual(["p2", "p1"]);
  });

  it("skips malformed events rather than counting them", () => {
    const events = [
      reaction("p1", "bell"),
      { post_id: "p1" } as ReactionEvent,
      { reaction_type: "bell" } as ReactionEvent,
      null as unknown as ReactionEvent,
    ];

    expect(summarizeReactions(events)).toEqual([
      { post_id: "p1", reaction_type: "bell", count: 1 },
    ]);
  });

  it("keeps the total count equal to the number of valid events", () => {
    const events = [
      reaction("p1", "bell"),
      reaction("p2", "heart"),
      reaction("p1", "bell"),
      reaction("p3", "celebrate"),
      reaction("p2", "heart"),
    ];

    const total = summarizeReactions(events).reduce((sum, n) => sum + n.count, 0);
    expect(total).toBe(events.length);
  });
});

describe("describeReactions", () => {
  it("returns null for an empty batch so no toast is shown", () => {
    expect(describeReactions([])).toBeNull();
  });

  it("describes a single reaction without a count", () => {
    const message = describeReactions([{ post_id: "p1", reaction_type: "bell", count: 1 }]);
    expect(message).toBe("Someone reacted 🔔 to your post");
  });

  it("includes the count and type when one group has several reactions", () => {
    const message = describeReactions([{ post_id: "p1", reaction_type: "heart", count: 4 }]);
    expect(message).toContain("4");
    expect(message).toContain("❤️");
  });

  it("totals across reaction types on a single post", () => {
    const message = describeReactions([
      { post_id: "p1", reaction_type: "bell", count: 2 },
      { post_id: "p1", reaction_type: "heart", count: 3 },
    ]);
    expect(message).toBe("5 reactions on your post");
  });

  it("names how many posts were reacted to when the batch spans several", () => {
    const message = describeReactions([
      { post_id: "p1", reaction_type: "bell", count: 2 },
      { post_id: "p2", reaction_type: "bell", count: 1 },
    ]);
    expect(message).toBe("3 reactions across 2 of your posts");
  });
});

describe("reactionEmoji", () => {
  it.each([
    ["bell", "🔔"],
    ["heart", "❤️"],
    ["celebrate", "🎉"],
  ])("maps %s", (type, emoji) => {
    expect(reactionEmoji(type)).toBe(emoji);
  });

  it("falls back for an unknown type rather than rendering undefined", () => {
    expect(reactionEmoji("brand-new-reaction")).toBe("👍");
  });
});

describe("mergePendingPosts", () => {
  it("puts newly arrived posts ahead of those already waiting", () => {
    const got = mergePendingPosts([post("new")], [post("older")], new Set());
    expect(got.map((p) => p.id)).toEqual(["new", "older"]);
  });

  it("drops posts already rendered in the feed", () => {
    const got = mergePendingPosts([post("seen"), post("fresh")], [], new Set(["seen"]));
    expect(got.map((p) => p.id)).toEqual(["fresh"]);
  });

  // A post can arrive over SSE and then also through pagination before the
  // buffer flushes; re-checking at merge time is what stops it rendering twice.
  it("removes a buffered post that arrived through pagination in the meantime", () => {
    const got = mergePendingPosts([], [post("p1"), post("p2")], new Set(["p1"]));
    expect(got.map((p) => p.id)).toEqual(["p2"]);
  });

  it("deduplicates a post present in both incoming and pending", () => {
    const got = mergePendingPosts([post("dup")], [post("dup")], new Set());
    expect(got.map((p) => p.id)).toEqual(["dup"]);
  });

  it("skips malformed entries", () => {
    const got = mergePendingPosts(
      [post("ok"), null as unknown as Post, {} as Post],
      [],
      new Set(),
    );
    expect(got.map((p) => p.id)).toEqual(["ok"]);
  });

  it("returns an empty list when everything is already known", () => {
    expect(mergePendingPosts([post("a")], [post("b")], new Set(["a", "b"]))).toEqual([]);
  });
});

describe("parseEventData", () => {
  it("parses a JSON object", () => {
    expect(parseEventData<{ id: string }>('{"id":"p1"}')).toEqual({ id: "p1" });
  });

  it("returns null for malformed JSON instead of throwing", () => {
    expect(parseEventData("{not json")).toBeNull();
  });

  it("returns null for a JSON scalar, which is never a valid event", () => {
    expect(parseEventData("42")).toBeNull();
    expect(parseEventData('"a string"')).toBeNull();
    expect(parseEventData("null")).toBeNull();
  });

  it("returns null for an empty payload", () => {
    expect(parseEventData("")).toBeNull();
  });
});
