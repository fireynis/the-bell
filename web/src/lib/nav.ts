import type { User } from "../api/types";
import { postingBlockReason, type Actor } from "./gating";
import { hasMinRole, type Role } from "./trust";

export interface NavItem {
  label: string;
  path: string;
  /** SVG path data for the 24x24 icon. */
  icon: string;
  /** Match the current path exactly; used by "/" so it is not always active. */
  exact?: boolean;
  /** Lowest role the item is shown to; omitted means everyone sees it. */
  minRole?: Role;
  /**
   * The capability the destination needs. Unlike minRole this does not hide the
   * item — see navItemLocked for why it is marked instead.
   */
  gate?: "post";
}

/**
 * The town's navigation, defined once. The sidebar and the bottom bar show
 * overlapping subsets in different orders, so they compose from these rather
 * than each carrying their own copy of every label, icon and role rule — which
 * is how the two drifted apart in the first place.
 */
export const NAV_ITEMS = {
  feed: {
    label: "Feed",
    path: "/",
    icon: "M3 12l2-2m0 0l7-7 7 7M5 10v10a1 1 0 001 1h3m10-11l2 2m-2-2v10a1 1 0 01-1 1h-3m-4 0a1 1 0 01-1-1v-4a1 1 0 011-1h2a1 1 0 011 1v4a1 1 0 01-1 1",
    exact: true,
  },
  compose: {
    label: "Post",
    path: "/compose",
    icon: "M12 4v16m8-8H4",
    gate: "post",
  },
  profile: {
    label: "Profile",
    path: "/profile",
    icon: "M16 7a4 4 0 11-8 0 4 4 0 018 0zM12 14a7 7 0 00-7 7h14a7 7 0 00-7-7z",
  },
  moderation: {
    label: "Moderation",
    path: "/moderation",
    icon: "M9 12l2 2 4-4m5.618-4.016A11.955 11.955 0 0112 2.944a11.955 11.955 0 01-8.618 3.04A12.02 12.02 0 003 9c0 5.591 3.824 10.29 9 11.622 5.176-1.332 9-6.03 9-11.622 0-1.042-.133-2.052-.382-3.016z",
    minRole: "moderator",
  },
  admin: {
    label: "Town Hall",
    path: "/admin",
    icon: "M19 21V5a2 2 0 00-2-2H7a2 2 0 00-2 2v16m14 0h2m-2 0h-5m-9 0H3m2 0h5M9 7h1m-1 4h1m4-4h1m-1 4h1m-5 10v-5a1 1 0 011-1h2a1 1 0 011 1v5m-4 0h4",
    minRole: "council",
  },
  settings: {
    label: "Settings",
    path: "/auth/settings",
    icon: "M10.325 4.317c.426-1.756 2.924-1.756 3.35 0a1.724 1.724 0 002.573 1.066c1.543-.94 3.31.826 2.37 2.37a1.724 1.724 0 001.066 2.573c1.756.426 1.756 2.924 0 3.35a1.724 1.724 0 00-1.066 2.573c.94 1.543-.826 3.31-2.37 2.37a1.724 1.724 0 00-2.573 1.066c-.426 1.756-2.924 1.756-3.35 0a1.724 1.724 0 00-2.573-1.066c-1.543.94-3.31-.826-2.37-2.37a1.724 1.724 0 00-1.066-2.573c-1.756-.426-1.756-2.924 0-3.35a1.724 1.724 0 001.066-2.573c-.94-1.543.826-3.31 2.37-2.37.996.608 2.296.07 2.572-1.065z M15 12a3 3 0 11-6 0 3 3 0 016 0z",
  },
} as const satisfies Record<string, NavItem>;

/** The wide sidebar; composing has its own button there, so it is not listed. */
export const SIDEBAR_NAV_ITEMS: readonly NavItem[] = [
  NAV_ITEMS.feed,
  NAV_ITEMS.profile,
  NAV_ITEMS.moderation,
  NAV_ITEMS.admin,
  NAV_ITEMS.settings,
];

/** The narrow bottom bar, which has room for composing but not for Town Hall. */
export const BOTTOM_NAV_ITEMS: readonly NavItem[] = [
  NAV_ITEMS.feed,
  NAV_ITEMS.compose,
  NAV_ITEMS.moderation,
  NAV_ITEMS.profile,
  NAV_ITEMS.settings,
];

/**
 * isActive reports whether a nav item should be highlighted.
 *
 * Matching by prefix keeps the item lit on its sub-pages, so "/moderation" is
 * still highlighted while reading "/moderation/user/123". The feed opts out of
 * that, since "/" prefixes every route and would otherwise always be active.
 */
export function isActive(path: string, currentPath: string, exact?: boolean): boolean {
  if (exact) return currentPath === path;
  return currentPath.startsWith(path);
}

/**
 * visibleNavItems drops the items a user may not use.
 *
 * This is presentation only — hiding a link is not access control, and the
 * server remains the authority on every route behind it. Hiding is still worth
 * doing: offering a link that answers with a redirect reads as a broken app.
 */
export function visibleNavItems(
  items: readonly NavItem[],
  user: Pick<User, "role" | "is_active"> | null,
): NavItem[] {
  return items.filter((item) => !item.minRole || hasMinRole(user, item.minRole));
}

/**
 * navItemLocked reports whether a user can see an item but cannot yet use what
 * it leads to.
 *
 * Locked is deliberately not hidden, which is the opposite of what minRole
 * does. A gated destination is how somebody learns the feature exists and what
 * it will take to reach it — hiding the composer from a pending member leaves
 * them not knowing the town has a bell. But an unmarked link is how they found
 * out only after writing a post they could not send, so the marking is the
 * whole point: the answer arrives before the effort, not after it.
 *
 * The reason itself is not returned here. It belongs to the page, which has the
 * room to explain it; a nav item only has room to say "not yet".
 */
export function navItemLocked(item: NavItem, user: Actor): boolean {
  if (item.gate !== "post") return false;
  return postingBlockReason(user) !== null;
}
