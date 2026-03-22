import { useState } from "react";
import { useNavigate } from "react-router";
import { api } from "../api/client";
import type { ApiError, Post } from "../api/types";
import ErrorBanner from "../components/ErrorBanner.tsx";
import Spinner from "../components/Spinner.tsx";

const MAX_LENGTH = 1000;
const WARN_THRESHOLD = 950;

export default function Compose() {
  const navigate = useNavigate();
  const [body, setBody] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const canSubmit = body.trim().length > 0 && !submitting;

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (!canSubmit) return;

    setSubmitting(true);
    setError(null);

    try {
      await api.post<Post>("/posts/", { body: body.trim() });
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
          <div
            className="mt-3 flex items-center justify-between border-t pt-3"
            style={{ borderColor: "var(--color-border-light)" }}
          >
            <p
              className="text-xs"
              style={{
                color: body.length >= WARN_THRESHOLD ? "var(--color-danger)" : "var(--color-text-tertiary)",
              }}
            >
              {body.length} / {MAX_LENGTH}
            </p>
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
