import type { FeedResponse, Post } from "../api/types";
import { appendFeedPage, replacePost, withoutPost } from "./post";

/** Everything the feed hook needs to render and to ask for the next page. */
export interface FeedState {
  posts: Post[];
  cursor?: string;
  hasMore: boolean;
}

/**
 * initialFeedState starts with hasMore true so the first page is fetched
 * without a cursor; only the server's reply can say the feed is exhausted.
 */
export function initialFeedState(): FeedState {
  return { posts: [], cursor: undefined, hasMore: true };
}

/**
 * applyFeedPage folds a page of results into the feed, appending for a cursor
 * fetch and replacing for a fresh one.
 *
 * `hasMore` is derived from next_cursor rather than tracked separately, so the
 * feed cannot end up asking for a page it has no cursor for — the two always
 * agree because they come from the same field.
 *
 * A malformed or empty response is treated as an empty page, which ends the
 * feed rather than throwing part-way through a render.
 */
export function applyFeedPage(state: FeedState, page: FeedResponse, append: boolean): FeedState {
  const incoming = Array.isArray(page?.posts) ? page.posts : [];
  const cursor = page?.next_cursor;

  return {
    posts: appendFeedPage(append ? state.posts : [], incoming),
    cursor,
    hasMore: !!cursor,
  };
}

/**
 * replaceFeedPost folds an author's edit into the loaded feed.
 *
 * The cursor and hasMore are carried through untouched: an edit changes a post's
 * body, not the feed's shape, and resetting either would make the next scroll
 * re-fetch a page the reader already has.
 */
export function replaceFeedPost(state: FeedState, updated: Post): FeedState {
  return { ...state, posts: replacePost(state.posts, updated) };
}

/**
 * removeFeedPost drops a post its author deleted.
 *
 * hasMore is likewise left alone. It answers "is there another page", which a
 * deletion does not change — the server's next cursor is still the last post the
 * reader was sent, whether or not this one is still in the list.
 */
export function removeFeedPost(state: FeedState, postId: string): FeedState {
  return { ...state, posts: withoutPost(state.posts, postId) };
}

/**
 * mergeLivePosts puts posts that arrived over SSE above the ones fetched by
 * page, dropping any that are in both.
 *
 * A live post is normally also in the first page, since it is the newest thing
 * in the feed — without this it renders twice and React warns about duplicate
 * keys. The live copy wins so the post keeps the position the reader first saw
 * it in.
 */
export function mergeLivePosts(live: readonly Post[], loaded: readonly Post[]): Post[] {
  return appendFeedPage(appendFeedPage([], live), loaded);
}
