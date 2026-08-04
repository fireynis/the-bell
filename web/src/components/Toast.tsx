import { useEffect, useState } from "react";

/** Matches the duration-300 fade in the class list below. */
const FADE_MS = 300;

interface ToastProps {
  message: string;
  onDismiss: () => void;
  duration?: number;
}

export function Toast({ message, onDismiss, duration = 3000 }: ToastProps) {
  const [visible, setVisible] = useState(true);

  useEffect(() => {
    // Both timers are tracked: the fade-out is scheduled from inside the first
    // one, so clearing only the outer timer still let onDismiss fire after the
    // toast had unmounted.
    let fadeTimer: ReturnType<typeof setTimeout>;

    const timer = setTimeout(() => {
      setVisible(false);
      fadeTimer = setTimeout(onDismiss, FADE_MS);
    }, duration);

    return () => {
      clearTimeout(timer);
      clearTimeout(fadeTimer);
    };
  }, [duration, onDismiss]);

  return (
    <div
      className={`fixed bottom-4 right-4 z-50 px-4 py-3 rounded-lg shadow-lg
                  bg-white border border-gray-200 text-sm text-gray-700
                  transition-all duration-300 ${visible ? "opacity-100 translate-y-0" : "opacity-0 translate-y-2"}`}
    >
      {message}
    </div>
  );
}
