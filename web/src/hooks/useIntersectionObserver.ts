import { useEffect, type RefObject } from "react";

export function useIntersectionObserver(
  ref: RefObject<Element | null>,
  onIntersect: () => void,
  enabled = true,
) {
  useEffect(() => {
    if (!enabled || !ref.current) return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) {
          onIntersect();
        }
      },
      { threshold: 0.1 },
    );

    observer.observe(ref.current);
    return () => observer.disconnect();
  }, [ref, onIntersect, enabled]);
}
