import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { api } from "../api/client";
import type { ApiError, Post } from "../api/types";
import ErrorBanner from "../components/ErrorBanner.tsx";
import Spinner from "../components/Spinner.tsx";

const MAX_LENGTH = 1000;
const MAX_IMAGE_SIZE = 5 * 1024 * 1024; // 5 MB
const ACCEPTED_TYPES = ["image/jpeg", "image/png", "image/webp"];

function counterColor(len: number): string {
  if (len >= 980) return "var(--color-danger)";
  if (len >= 950) return "#ca8a04"; // yellow-600
  return "#16a34a"; // green-600
}

function counterOpacity(len: number): number {
  if (len < 900) return 0;
  // Fade in between 900-920
  if (len < 920) return (len - 900) / 20;
  return 1;
}

export default function Compose() {
  const navigate = useNavigate();
  const [body, setBody] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [image, setImage] = useState<File | null>(null);
  const [imagePreview, setImagePreview] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const canSubmit = body.trim().length > 0 && !submitting;

  // Clean up object URL on unmount or when preview changes
  useEffect(() => {
    return () => {
      if (imagePreview) {
        URL.revokeObjectURL(imagePreview);
      }
    };
  }, [imagePreview]);

  function handleFileSelect(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    if (!file) return;

    if (!ACCEPTED_TYPES.includes(file.type)) {
      setError("Invalid image type. Please use JPEG, PNG, or WebP.");
      e.target.value = "";
      return;
    }

    if (file.size > MAX_IMAGE_SIZE) {
      setError("Image must be under 5 MB.");
      e.target.value = "";
      return;
    }

    setError(null);

    // Revoke previous preview URL if any
    if (imagePreview) {
      URL.revokeObjectURL(imagePreview);
    }

    setImage(file);
    setImagePreview(URL.createObjectURL(file));
  }

  function removeImage() {
    if (imagePreview) {
      URL.revokeObjectURL(imagePreview);
    }
    setImage(null);
    setImagePreview(null);
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;

    setSubmitting(true);
    setError(null);

    try {
      if (image) {
        const formData = new FormData();
        formData.append("body", body.trim());
        formData.append("image", image);
        await api.upload<Post>("/posts/", formData);
      } else {
        await api.post<Post>("/posts/", { body: body.trim() });
      }
      navigate("/", { replace: true });
    } catch (err) {
      const apiErr = err as ApiError;
      setError(apiErr.error ?? "Failed to create post. Please try again.");
      setSubmitting(false);
    }
  }

  return (
    <div className="py-5">
      <h1
        className="mb-5 text-xl font-bold"
        style={{ fontFamily: "var(--font-display)", color: "var(--color-text)" }}
      >
        Ring the Bell
      </h1>

      {error && <div className="mb-4"><ErrorBanner message={error} /></div>}

      <form onSubmit={handleSubmit}>
        <div
          className="p-4"
          style={{
            backgroundColor: "var(--color-surface)",
            boxShadow: "var(--shadow-sm)",
            borderRadius: "var(--radius-lg)",
          }}
        >
          <textarea
            value={body}
            onChange={(e) => setBody(e.target.value)}
            maxLength={MAX_LENGTH}
            rows={6}
            placeholder="What's happening in town?"
            className="w-full resize-none border-0 bg-transparent leading-relaxed focus:outline-none"
            style={{ color: "var(--color-text)", fontSize: "0.9375rem" }}
          />

          {/* Image preview */}
          {imagePreview && (
            <div className="relative mt-2 inline-block">
              <img
                src={imagePreview}
                alt="Upload preview"
                className="rounded-lg object-cover"
                style={{ width: "96px", height: "96px" }}
              />
              <button
                type="button"
                onClick={removeImage}
                className="absolute -right-2 -top-2 flex h-6 w-6 items-center justify-center rounded-full text-xs font-bold text-white shadow-md"
                style={{ backgroundColor: "var(--color-danger)" }}
                aria-label="Remove image"
              >
                &times;
              </button>
            </div>
          )}

          <div
            className="mt-3 flex items-center justify-between border-t pt-3"
            style={{ borderColor: "var(--color-border-light)" }}
          >
            <div className="flex items-center gap-3">
              {/* Image upload button */}
              <input
                ref={fileInputRef}
                type="file"
                accept="image/jpeg,image/png,image/webp"
                onChange={handleFileSelect}
                className="hidden"
              />
              <button
                type="button"
                onClick={() => fileInputRef.current?.click()}
                className="flex items-center justify-center rounded-md p-1.5 transition-colors hover:opacity-70"
                style={{ color: "var(--color-text-tertiary)" }}
                aria-label="Attach image"
                title="Attach image"
              >
                <svg
                  xmlns="http://www.w3.org/2000/svg"
                  width="20"
                  height="20"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                >
                  <rect x="3" y="3" width="18" height="18" rx="2" ry="2" />
                  <circle cx="8.5" cy="8.5" r="1.5" />
                  <polyline points="21 15 16 10 5 21" />
                </svg>
              </button>

              {/* Progressive character counter */}
              <p
                className="text-xs"
                style={{
                  color: counterColor(body.length),
                  opacity: counterOpacity(body.length),
                  transition: "color 0.3s, opacity 0.3s",
                }}
              >
                {body.length} / {MAX_LENGTH}
              </p>
            </div>

            <button
              type="submit"
              disabled={!canSubmit}
              className="rounded-full px-5 py-2 text-sm font-semibold transition-opacity disabled:cursor-not-allowed disabled:opacity-40"
              style={{
                backgroundColor: "var(--color-primary)",
                color: "var(--color-text-inverse)",
              }}
            >
              {submitting ? (
                <span className="inline-flex items-center gap-2">
                  <Spinner size="sm" />
                  Posting...
                </span>
              ) : (
                "Post"
              )}
            </button>
          </div>
        </div>
      </form>
    </div>
  );
}
