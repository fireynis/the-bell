import { describe, expect, it } from "vitest";
import type { User } from "../api/types";
import {
  BOTTOM_NAV_ITEMS,
  NAV_ITEMS,
  SIDEBAR_NAV_ITEMS,
  isActive,
  navItemLocked,
  visibleNavItems,
  type NavItem,
} from "./nav";
import { POSTING_THRESHOLD } from "./trust";

function user(overrides: Partial<User> = {}): User {
  return {
    id: "u1",
    display_name: "Resident",
    bio: "",
    avatar_url: "",
    trust_score: 50,
    role: "member",
    is_active: true,
    joined_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

function labels(items: NavItem[]): string[] {
  return items.map((i) => i.label);
}

describe("isActive", () => {
  it("highlights the item for the current path", () => {
    expect(isActive("/profile", "/profile")).toBe(true);
  });

  // The item stays lit on its sub-pages, so a moderator reading one user's
  // history still sees where they are.
  it("stays highlighted on a sub-page", () => {
    expect(isActive("/moderation", "/moderation/user/123")).toBe(true);
  });

  it("does not highlight an unrelated path", () => {
    expect(isActive("/profile", "/admin")).toBe(false);
  });

  // "/" prefixes every route, so the feed must match exactly or it would be
  // highlighted everywhere in the app.
  it("matches the feed only on the feed itself", () => {
    expect(isActive("/", "/", true)).toBe(true);
    expect(isActive("/", "/compose", true)).toBe(false);
  });

  it("would highlight the feed everywhere without the exact flag", () => {
    expect(isActive("/", "/compose")).toBe(true);
  });
});

describe("visibleNavItems", () => {
  it("shows the unrestricted items to an ordinary member", () => {
    expect(labels(visibleNavItems(SIDEBAR_NAV_ITEMS, user()))).toEqual([
      "Feed",
      "Neighbours",
      "Profile",
      "Settings",
    ]);
  });

  it("adds moderation for a moderator", () => {
    expect(labels(visibleNavItems(SIDEBAR_NAV_ITEMS, user({ role: "moderator" })))).toContain(
      "Moderation",
    );
  });

  // Council outranks moderator, so they get the moderation queue as well as
  // the town hall.
  it("gives council everything", () => {
    expect(labels(visibleNavItems(SIDEBAR_NAV_ITEMS, user({ role: "council" })))).toEqual([
      "Feed",
      "Neighbours",
      "Profile",
      "Moderation",
      "Town Hall",
      "Settings",
    ]);
  });

  it("withholds the town hall from a moderator", () => {
    expect(labels(visibleNavItems(SIDEBAR_NAV_ITEMS, user({ role: "moderator" })))).not.toContain(
      "Town Hall",
    );
  });

  // Deactivation removes every capability, matching canModerate and isCouncil.
  it("withholds restricted items from a deactivated council member", () => {
    const items = visibleNavItems(SIDEBAR_NAV_ITEMS, user({ role: "council", is_active: false }));
    expect(labels(items)).toEqual(["Feed", "Neighbours", "Profile", "Settings"]);
  });

  // A role this build has never heard of grants nothing beyond the basics.
  it.each(["archivist", "", "MODERATOR"])("treats the unknown role %o as ordinary", (role) => {
    expect(labels(visibleNavItems(SIDEBAR_NAV_ITEMS, user({ role })))).toEqual([
      "Feed",
      "Neighbours",
      "Profile",
      "Settings",
    ]);
  });

  it.each(["pending", "banned"])("shows a %s user only the unrestricted items", (role) => {
    expect(labels(visibleNavItems(SIDEBAR_NAV_ITEMS, user({ role })))).toEqual([
      "Feed",
      "Neighbours",
      "Profile",
      "Settings",
    ]);
  });

  it("shows only the unrestricted items when nobody is signed in", () => {
    expect(labels(visibleNavItems(SIDEBAR_NAV_ITEMS, null))).toEqual([
      "Feed",
      "Neighbours",
      "Profile",
      "Settings",
    ]);
  });

  it("filters the bottom bar by the same rules", () => {
    expect(labels(visibleNavItems(BOTTOM_NAV_ITEMS, user()))).toEqual([
      "Feed",
      "Neighbours",
      "Post",
      "Profile",
    ]);
    expect(labels(visibleNavItems(BOTTOM_NAV_ITEMS, user({ role: "moderator" })))).toContain(
      "Moderation",
    );
  });

  // Five items is what fits at 360px, and a moderator sees every one of them.
  // Settings is the item that gave way; AppLayout's mobile header carries it,
  // since the sidebar that carries it elsewhere is desktop-only.
  it("keeps the bottom bar to five items for a moderator", () => {
    const items = visibleNavItems(BOTTOM_NAV_ITEMS, user({ role: "moderator" }));
    expect(labels(items)).toEqual(["Feed", "Neighbours", "Post", "Moderation", "Profile"]);
  });

  it("leaves settings out of the bottom bar", () => {
    expect(BOTTOM_NAV_ITEMS).not.toContain(NAV_ITEMS.settings);
    expect(SIDEBAR_NAV_ITEMS).toContain(NAV_ITEMS.settings);
  });

  // The directory is how a pending member finds somebody who knows them, which
  // is the one thing that ends their being pending — so it is in both bars for
  // them, and behind neither a role nor a gate.
  it.each([
    ["the sidebar", SIDEBAR_NAV_ITEMS],
    ["the bottom bar", BOTTOM_NAV_ITEMS],
  ])("keeps the directory in %s for a pending member", (_name, items) => {
    expect(labels(visibleNavItems(items, user({ role: "pending" })))).toContain("Neighbours");
  });

  it("does not mutate the list it was given", () => {
    const before = SIDEBAR_NAV_ITEMS.length;
    visibleNavItems(SIDEBAR_NAV_ITEMS, user());
    expect(SIDEBAR_NAV_ITEMS).toHaveLength(before);
  });
});

// Locked is not hidden, and the difference is the whole point: a pending member
// used to see an unmarked "Post" item and find out about the rule only after
// writing something they could not send.
describe("navItemLocked", () => {
  it("locks the composer for a pending member", () => {
    expect(navItemLocked(NAV_ITEMS.compose, user({ role: "pending" }))).toBe(true);
  });

  it("locks the composer for a member below the posting threshold", () => {
    expect(
      navItemLocked(NAV_ITEMS.compose, user({ trust_score: POSTING_THRESHOLD - 1 })),
    ).toBe(true);
  });

  it("leaves the composer open to a member who can post", () => {
    expect(navItemLocked(NAV_ITEMS.compose, user({ trust_score: POSTING_THRESHOLD }))).toBe(
      false,
    );
  });

  it("locks the composer when nobody is signed in", () => {
    expect(navItemLocked(NAV_ITEMS.compose, null)).toBe(true);
  });

  // A locked item is still shown; only minRole removes anything.
  it("keeps the composer in the bottom bar for a pending member", () => {
    const items = visibleNavItems(BOTTOM_NAV_ITEMS, user({ role: "pending" }));
    expect(labels(items)).toContain("Post");
  });

  it.each([
    ["feed", NAV_ITEMS.feed],
    ["profile", NAV_ITEMS.profile],
    ["settings", NAV_ITEMS.settings],
    ["neighbors", NAV_ITEMS.neighbors],
    ["moderation", NAV_ITEMS.moderation],
  ])("never locks %s, which needs nothing of the trust graph", (_name, item) => {
    expect(navItemLocked(item, user({ role: "pending" }))).toBe(false);
  });
});

describe("the nav model", () => {
  // Both bars render items by path as the React key.
  it.each([
    ["the sidebar", SIDEBAR_NAV_ITEMS],
    ["the bottom bar", BOTTOM_NAV_ITEMS],
  ])("gives %s a unique path per item", (_name, items) => {
    expect(new Set(items.map((i) => i.path)).size).toBe(items.length);
  });

  // The two bars used to hold their own copies of these strings and drifted;
  // sharing one definition is the point of this module.
  it("shares one definition between the two bars", () => {
    expect(SIDEBAR_NAV_ITEMS[0]).toBe(BOTTOM_NAV_ITEMS[0]);
    expect(SIDEBAR_NAV_ITEMS).toContain(NAV_ITEMS.moderation);
    expect(BOTTOM_NAV_ITEMS).toContain(NAV_ITEMS.moderation);
  });

  it("gives every item an icon and a label", () => {
    for (const item of Object.values(NAV_ITEMS)) {
      expect(item.icon.length).toBeGreaterThan(0);
      expect(item.label.length).toBeGreaterThan(0);
    }
  });
});
