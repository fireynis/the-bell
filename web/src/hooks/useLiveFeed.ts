import { useCallback, useEffect, useRef, useState } from "react";
import { Post } from "../api/types";

const DEBOUNCE_MS = 15_000;

export interface ReactionNotification {
  post_id: string;
  reaction_type: string;
  count: number;
}

interface UseLiveFeedReturn {
  pendingPosts: Post[];
  pendingCount: number;
  flush: () => void;
}

export function useLiveFeed(
  existingPostIds: Set<string>,
  onReaction?: (event: ReactionNotification) => void,
): UseLiveFeedReturn {
  const [pendingPosts, setPendingPosts] = useState<Post[]>([]);
  const bufferRef = useRef<Post[]>([]);
  const reactionBufferRef = useRef<ReactionNotification[]>([]);
  const timerRef = useRef<ReturnType<typeof setInterval>>();
  const existingIdsRef = useRef(existingPostIds);
  existingIdsRef.current = existingPostIds;
  const onReactionRef = useRef(onReaction);
  onReactionRef.current = onReaction;

  useEffect(() => {
    const eventSource = new EventSource("/api/v1/feed/live");

    eventSource.addEventListener("new_post", (e: MessageEvent) => {
      try {
        const post: Post = JSON.parse(e.data);
        if (!existingIdsRef.current.has(post.id)) {
          bufferRef.current.push(post);
        }
      } catch {
        // ignore malformed events
      }
    });

    eventSource.addEventListener("reaction", (e: MessageEvent) => {
      try {
        const data = JSON.parse(e.data);
        reactionBufferRef.current.push(data);
      } catch {
        // ignore
      }
    });

    // Flush buffer every 15s
    timerRef.current = setInterval(() => {
      if (bufferRef.current.length > 0) {
        setPendingPosts((prev) => [...bufferRef.current, ...prev]);
        bufferRef.current = [];
      }
      if (reactionBufferRef.current.length > 0 && onReactionRef.current) {
        onReactionRef.current({
          post_id: reactionBufferRef.current[0].post_id,
          reaction_type: reactionBufferRef.current[0].reaction_type,
          count: reactionBufferRef.current.length,
        });
        reactionBufferRef.current = [];
      }
    }, DEBOUNCE_MS);

    eventSource.onerror = () => {
      // EventSource auto-reconnects by default
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
