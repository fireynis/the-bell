import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { api } from "../api/client";
import type { ApiError, Post } from "../api/types";

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
    <div className="mx-auto max-w-2xl p-4">
      <div className="mb-6 flex items-center gap-3">
        <Link
          to="/"
          className="text-sm text-indigo-600 hover:text-indigo-500"
        >
          &larr; Back
        </Link>
        <h1 className="text-2xl font-bold">New Post</h1>
      </div>

      {error && (
        <div className="mb-4 rounded-md bg-red-50 p-3 text-sm text-red-700">
          {error}
        </div>
      )}

      <form onSubmit={handleSubmit}>
        <textarea
          value={body}
          onChange={(e) => setBody(e.target.value)}
          maxLength={MAX_LENGTH}
          rows={6}
          placeholder="What's on your mind?"
          className="w-full rounded-md border border-gray-300 p-3 text-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
        />
        <p
          className={`mt-1 text-right text-xs ${
            body.length >= WARN_THRESHOLD ? "text-red-600" : "text-gray-400"
          }`}
        >
          {body.length} / {MAX_LENGTH}
        </p>

        <button
          type="submit"
          disabled={!canSubmit}
          className="mt-4 w-full rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {submitting ? (
            <span className="inline-flex items-center gap-2">
              <span className="h-4 w-4 animate-spin rounded-full border-2 border-white border-t-transparent" />
              Posting…
            </span>
          ) : (
            "Post"
          )}
        </button>
      </form>
    </div>
  );
}
