import { describe, expect, it } from "vitest";
import type { User } from "../api/types";
import {
  BOTTOM_NAV_ITEMS,
  NAV_ITEMS,
  SIDEBAR_NAV_ITEMS,
  isActive,
  visibleNavItems,
  type NavItem,
} from "./nav";

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
    expect(labels(items)).toEqual(["Feed", "Profile", "Settings"]);
  });

  // A role this build has never heard of grants nothing beyond the basics.
  it.each(["archivist", "", "MODERATOR"])("treats the unknown role %o as ordinary", (role) => {
    expect(labels(visibleNavItems(SIDEBAR_NAV_ITEMS, user({ role })))).toEqual([
      "Feed",
      "Profile",
      "Settings",
    ]);
  });

  it.each(["pending", "banned"])("shows a %s user only the unrestricted items", (role) => {
    expect(labels(visibleNavItems(SIDEBAR_NAV_ITEMS, user({ role })))).toEqual([
      "Feed",
      "Profile",
      "Settings",
    ]);
  });

  it("shows only the unrestricted items when nobody is signed in", () => {
    expect(labels(visibleNavItems(SIDEBAR_NAV_ITEMS, null))).toEqual([
      "Feed",
      "Profile",
      "Settings",
    ]);
  });

  it("filters the bottom bar by the same rules", () => {
    expect(labels(visibleNavItems(BOTTOM_NAV_ITEMS, user()))).toEqual([
      "Feed",
      "Post",
      "Profile",
      "Settings",
    ]);
    expect(labels(visibleNavItems(BOTTOM_NAV_ITEMS, user({ role: "moderator" })))).toContain(
      "Moderation",
    );
  });

  it("does not mutate the list it was given", () => {
    const before = SIDEBAR_NAV_ITEMS.length;
    visibleNavItems(SIDEBAR_NAV_ITEMS, user());
    expect(SIDEBAR_NAV_ITEMS).toHaveLength(before);
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
