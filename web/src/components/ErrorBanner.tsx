interface ErrorBannerProps {
  message: string;
  onRetry?: () => void;
}

export default function ErrorBanner({ message, onRetry }: ErrorBannerProps) {
  return (
    <div
      className="rounded-[var(--radius-md)] p-3 text-sm"
      style={{ backgroundColor: "var(--color-danger-light)", color: "var(--color-danger)" }}
    >
      {message}
      {onRetry && (
        <button onClick={onRetry} className="ml-2 font-semibold underline">
          Retry
        </button>
      )}
    </div>
  );
}
