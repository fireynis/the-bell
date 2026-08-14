import { useModalDialog } from "../hooks/useModalDialog.ts";

interface ImageLightboxProps {
  src: string;
  alt?: string;
  onClose: () => void;
}

/**
 * Shows a post's image at full size.
 *
 * It goes through useModalDialog like every other dialog: it used to handle
 * Escape itself and nothing else, so focus stayed on the page underneath and a
 * keyboard user could tab through a feed they could no longer see. The close
 * button is what makes the trap possible — a panel with nothing focusable in it
 * has nowhere to put focus, and no way out that does not involve a mouse.
 */
export function ImageLightbox({ src, alt, onClose }: ImageLightboxProps) {
  const panelRef = useModalDialog<HTMLDivElement>(onClose);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 p-4"
      role="dialog"
      aria-modal="true"
      aria-label="Image"
      onClick={onClose}
    >
      <div
        ref={panelRef}
        className="relative max-h-full"
        onClick={(e) => e.stopPropagation()}
      >
        <img
          src={src}
          alt={alt || ""}
          className="max-w-[90vw] max-h-[90vh] rounded-lg object-contain"
        />
        <button
          type="button"
          onClick={onClose}
          aria-label="Close image"
          className="absolute right-2 top-2 flex h-9 w-9 items-center justify-center rounded-full bg-black/60 text-lg leading-none text-white"
        >
          <span aria-hidden="true">&times;</span>
        </button>
      </div>
    </div>
  );
}
