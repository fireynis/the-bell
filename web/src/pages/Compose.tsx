import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router";
import { postApi } from "../api/client";
import { useAuth } from "../context/AuthContext";
import ErrorBanner from "../components/ErrorBanner.tsx";
import Spinner from "../components/Spinner.tsx";
import {
  ALLOWED_IMAGE_TYPES,
  MAX_ALT_TEXT_LENGTH,
  MAX_POST_BODY_LENGTH,
  counterColor,
  counterOpacity,
  remainingAltTextChars,
  remainingChars,
  runeLength,
  validateAltText,
  validateImageFile,
  validatePostBody,
} from "../lib/post.ts";
import { awaitingWelcome, postingBlockReason } from "../lib/gating.ts";
import { apiErrorMessage } from "../lib/verification.ts";
import { RINGING, RING_THE_BELL } from "../lib/copy.ts";
import PendingNotice from "../components/PendingNotice.tsx";

export default function Compose() {
  const navigate = useNavigate();
  const { user, profileError } = useAuth();
  const [body, setBody] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [image, setImage] = useState<File | null>(null);
  const [imagePreview, setImagePreview] = useState<string | null>(null);
  const [altText, setAltText] = useState("");
  const fileInputRef = useRef<HTMLInputElement>(null);

  // Null when the user may post. The server is still the authority; this only
  // stops someone writing a whole post and submitting it into a certain 403.
  //
  // profileError is passed so that a member whose profile failed to load is not
  // told to sign in — they already are, and the sign-in they would go and do
  // lands them back here with the same message.
  const postBlock = postingBlockReason(user, { profileUnavailable: profileError });

  const bodyCheck = validatePostBody(body);
  const altCheck = validateAltText(altText, image !== null);
  const canSubmit = bodyCheck.valid && altCheck.valid && !submitting && postBlock === null;
  const remaining = remainingChars(body);
  const altRemaining = remainingAltTextChars(altText);

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

    const check = validateImageFile(file);
    if (!check.valid) {
      setError(check.error ?? null);
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
    // The description goes with the image it described. Keeping it would leave
    // the post carrying a sentence about a picture that is no longer attached —
    // which the server rejects outright, so the alternative is a submit that
    // fails for a reason nothing on screen explains.
    setAltText("");
    if (fileInputRef.current) {
      fileInputRef.current.value = "";
    }
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    if (submitting) return;
    if (postBlock) {
      setError(postBlock);
      return;
    }
    if (!bodyCheck.valid) {
      setError(bodyCheck.error ?? null);
      return;
    }
    if (!altCheck.valid) {
      setError(altCheck.error ?? null);
      return;
    }

    setSubmitting(true);
    setError(null);

    try {
      if (image) {
        const formData = new FormData();
        formData.append("body", body.trim());
        formData.append("image", image);
        formData.append("alt_text", altText.trim());
        await postApi.createWithImage(formData);
      } else {
        await postApi.create({ body: body.trim() });
      }
      navigate("/", { replace: true });
    } catch (err) {
      setError(apiErrorMessage(err, "Failed to create post. Please try again."));
      setSubmitting(false);
    }
  }

  return (
    <div className="py-5">
      <h1
        className="mb-5 text-xl font-bold"
        style={{ fontFamily: "var(--font-display)", color: "var(--color-text)" }}
      >
        {RING_THE_BELL}
      </h1>

      {error && <div className="mb-4"><ErrorBanner message={error} /></div>}

      {/*
        Shown rather than hiding the composer. Earning trust through vouches is
        the platform's core mechanic, so a member who cannot post yet should see
        what the box is and what it will take to use it — hiding it would leave
        them with no idea the feature exists, let alone how to unlock it.

        A newcomer gets the long version, the same one the feed greeted them
        with. Arriving here from a shortcut marked "you cannot ring the bell
        yet" and finding one terse line would be the dead end again, just
        signposted; the welcome is what makes the two halves one journey.
      */}
      {postBlock &&
        (awaitingWelcome(user) ? (
          <PendingNotice className="mb-4" />
        ) : (
          <div
            className="mb-4 rounded-[var(--radius-md)] p-3 text-sm"
            style={{
              backgroundColor: "var(--color-warning-light, var(--color-surface-tertiary))",
              color: "var(--color-text-secondary)",
            }}
            role="status"
          >
            {postBlock}
          </div>
        ))}

      <form onSubmit={handleSubmit}>
        <div
          className="p-4"
          style={{
            backgroundColor: "var(--color-surface)",
            boxShadow: "var(--shadow-sm)",
            borderRadius: "var(--radius-lg)",
          }}
        >
          {/*
            Disabled rather than merely unsubmittable: the point is to spend the
            user's effort only where it can succeed, and a box that accepts a
            thousand characters and then refuses to send them is the bug this
            fixes, not a milder version of it.
          */}
          <textarea
            value={body}
            onChange={(e) => setBody(e.target.value)}
            rows={6}
            disabled={postBlock !== null}
            placeholder={
              postBlock ? "You cannot post yet" : "What's happening in town?"
            }
            // Set in the same serif the post will be read in, so the composer
            // shows the shape of what is about to be said rather than a
            // different, smaller version of it. No focus:outline-none: the
            // box is borderless, so the app's focus ring is the only thing
            // that tells a keyboard user the caret landed here.
            className="post-body w-full resize-none border-0 bg-transparent disabled:cursor-not-allowed disabled:opacity-60"
          />

          {/* Image preview */}
          {imagePreview && (
            <div className="mt-2">
              <div className="relative inline-block">
                <img
                  src={imagePreview}
                  alt="Upload preview"
                  className="rounded-lg object-cover"
                  style={{ width: "96px", height: "96px" }}
                />
                <button
                  type="button"
                  onClick={removeImage}
                  className="absolute -right-2 -top-2 flex h-6 w-6 items-center justify-center rounded-full text-xs font-bold shadow-md"
                  style={{
                    backgroundColor: "var(--color-danger)",
                    color: "var(--color-text-inverse)",
                  }}
                  aria-label="Remove image"
                >
                  &times;
                </button>
              </div>

              {/*
                Only once there is an image to describe, and never as a gate on
                posting: the field is optional and the submit button does not
                wait for it. What it does do is sit in the way — under the
                preview, labelled and explained — because the whole reason every
                image in this feed used to reach a screen reader as silence is
                that nothing ever asked.
              */}
              <div className="mt-3">
                <label
                  htmlFor="compose-alt-text"
                  className="mb-1 block text-sm font-medium"
                  style={{ color: "var(--color-text-secondary)" }}
                >
                  Describe this image
                </label>
                <textarea
                  id="compose-alt-text"
                  value={altText}
                  onChange={(e) => setAltText(e.target.value)}
                  rows={2}
                  disabled={postBlock !== null}
                  placeholder="The bandstand in Wilson Park, freshly painted green"
                  aria-describedby="compose-alt-text-help"
                  className="field w-full resize-none rounded-[var(--radius-md)] px-3 py-2 text-sm disabled:cursor-not-allowed disabled:opacity-60"
                />
                <div className="mt-1 flex items-baseline justify-between gap-3">
                  <p
                    id="compose-alt-text-help"
                    className="text-xs"
                    style={{ color: "var(--color-text-tertiary)" }}
                  >
                    For neighbours using screen readers.
                  </p>
                  {/* The composer's own counter, on this field's limit: quiet
                      until the limit is close, then increasingly insistent. */}
                  <p
                    className="shrink-0 text-xs"
                    style={{
                      color: counterColor(altRemaining),
                      opacity: counterOpacity(altRemaining),
                      transition: "color 0.3s, opacity 0.3s",
                    }}
                  >
                    {runeLength(altText)} / {MAX_ALT_TEXT_LENGTH}
                  </p>
                </div>
              </div>
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
                accept={ALLOWED_IMAGE_TYPES.join(",")}
                onChange={handleFileSelect}
                className="hidden"
              />
              <button
                type="button"
                onClick={() => fileInputRef.current?.click()}
                disabled={postBlock !== null}
                className="flex items-center justify-center rounded-md p-1.5 transition-colors hover:opacity-70 disabled:opacity-40"
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
                  color: counterColor(remaining),
                  opacity: counterOpacity(remaining),
                  transition: "color 0.3s, opacity 0.3s",
                }}
              >
                {body.length} / {MAX_POST_BODY_LENGTH}
              </p>
            </div>

            {/* The same words as the page title and the sidebar button: one
                act, one name, all the way through. */}
            <button
              type="submit"
              disabled={!canSubmit}
              className="btn btn-primary rounded-full px-5 py-2 text-sm font-semibold disabled:opacity-40"
            >
              {submitting ? (
                <span className="inline-flex items-center gap-2">
                  <Spinner size="sm" />
                  {RINGING}
                </span>
              ) : (
                RING_THE_BELL
              )}
            </button>
          </div>
        </div>
      </form>
    </div>
  );
}
