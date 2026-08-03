import { useCallback, useEffect, useRef, useState } from "react";
import type { Post } from "../api/types";
import {
  mergePendingPosts,
  parseEventData,
  summarizeReactions,
  type ReactionEvent,
  type ReactionNotification,
} from "../lib/liveFeed";

const DEBOUNCE_MS = 15_000;

export type { ReactionNotification };

interface UseLiveFeedReturn {
  pendingPosts: Post[];
  pendingCount: number;
  flush: () => void;
}

/**
 * @param existingPostIds IDs already rendered, so live arrivals are not shown twice.
 * @param onReactions called once per flush with every distinct post/reaction
 *   pair in the batch. It receives the whole batch rather than one call per
 *   group so the caller can render a single summary and play a single sound.
 */
export function useLiveFeed(
  existingPostIds: Set<string>,
  onReactions?: (notifications: ReactionNotification[]) => void,
): UseLiveFeedReturn {
  const [pendingPosts, setPendingPosts] = useState<Post[]>([]);
  const bufferRef = useRef<Post[]>([]);
  const reactionBufferRef = useRef<ReactionEvent[]>([]);
  const timerRef = useRef<ReturnType<typeof setInterval>>(undefined);

  // Kept in refs so the subscription effect below can stay mounted for the
  // lifetime of the component: re-running it would tear down and re-open the
  // EventSource on every render that changes the feed or the callback.
  // They are assigned in an effect rather than during render, since writing to
  // a ref while rendering is not safe under concurrent rendering.
  const existingIdsRef = useRef(existingPostIds);
  const onReactionsRef = useRef(onReactions);

  useEffect(() => {
    existingIdsRef.current = existingPostIds;
    onReactionsRef.current = onReactions;
  });

  useEffect(() => {
    const eventSource = new EventSource("/api/v1/feed/live");

    eventSource.addEventListener("new_post", (e: MessageEvent) => {
      const post = parseEventData<Post>(e.data);
      if (post && !existingIdsRef.current.has(post.id)) {
        bufferRef.current.push(post);
      }
    });

    eventSource.addEventListener("reaction", (e: MessageEvent) => {
      const reaction = parseEventData<ReactionEvent>(e.data);
      if (reaction) {
        reactionBufferRef.current.push(reaction);
      }
    });

    // Events are batched rather than applied immediately so the feed does not
    // reflow under the reader while they are part-way through a post.
    timerRef.current = setInterval(() => {
      if (bufferRef.current.length > 0) {
        const incoming = bufferRef.current;
        bufferRef.current = [];
        setPendingPosts((prev) => mergePendingPosts(incoming, prev, existingIdsRef.current));
      }

      if (reactionBufferRef.current.length > 0 && onReactionsRef.current) {
        const notifications = summarizeReactions(reactionBufferRef.current);
        reactionBufferRef.current = [];
        if (notifications.length > 0) {
          onReactionsRef.current(notifications);
        }
      }
    }, DEBOUNCE_MS);

    eventSource.onerror = () => {
      // EventSource reconnects on its own; a transient drop needs no handling.
    };

    return () => {
      eventSource.close();
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, []);

  const flush = useCallback(() => {
    setPendingPosts([]);
  }, []);

  return {
    pendingPosts,
    pendingCount: pendingPosts.length,
    flush,
  };
}
