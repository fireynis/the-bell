interface NewPostsBannerProps {
  count: number;
  onClick: () => void;
}

export function NewPostsBanner({ count, onClick }: NewPostsBannerProps) {
  if (count === 0) return null;

  return (
    <button
      onClick={onClick}
      className="w-full py-2 px-4 text-sm font-medium text-white rounded-lg mb-4
                 bg-[var(--color-primary)] hover:bg-[var(--color-primary-hover)]
                 transition-all duration-300 animate-slide-down cursor-pointer"
    >
      {count} new {count === 1 ? "post" : "posts"}
    </button>
  );
}
