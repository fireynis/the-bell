import { useState } from "react";
import { userApi } from "../../api/client";
import type { ApiError, User } from "../../api/types";
import ErrorBanner from "../../components/ErrorBanner";

/**
 * EditProfileForm edits the signed-in user's own profile in place, collapsing
 * to a single "Edit profile" link until it is opened.
 *
 * The save goes through userApi.updateProfile rather than api.put with an
 * object literal, so the body is checked against UpdateProfileRequest — the
 * shape the server actually parses — instead of being an untyped bag that only
 * fails at runtime if a field is renamed.
 */
export default function EditProfileForm({
  user,
  onSave,
}: {
  user: User;
  onSave: (updated: User) => void;
}) {
  const [displayName, setDisplayName] = useState(user.display_name);
  const [bio, setBio] = useState(user.bio);
  const [avatarUrl, setAvatarUrl] = useState(user.avatar_url);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [editing, setEditing] = useState(false);

  if (!editing) {
    return (
      <button
        onClick={() => setEditing(true)}
        className="text-sm"
        style={{ color: "var(--color-primary)" }}
      >
        Edit profile
      </button>
    );
  }

  async function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    setSaving(true);
    setError(null);

    try {
      const updated = await userApi.updateProfile({
        display_name: displayName.trim(),
        bio: bio.trim(),
        avatar_url: avatarUrl.trim(),
      });
      onSave(updated);
      setEditing(false);
    } catch (err) {
      const apiErr = err as ApiError;
      setError(apiErr.error ?? "Failed to update profile.");
    } finally {
      setSaving(false);
    }
  }

  // The border, the colours and the focus ring all come from `.field`, which
  // is where every other form in the app gets them.
  const inputClass = "field w-full rounded-md px-3 py-2 text-sm";

  return (
    <form onSubmit={handleSubmit} className="mt-4 space-y-3">
      {error && <ErrorBanner message={error} />}
      <div>
        <label
          className="mb-1 block text-sm font-medium"
          style={{ color: "var(--color-text-secondary)" }}
        >
          Display name
        </label>
        <input
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          maxLength={100}
          required
          className={inputClass}
        />
      </div>
      <div>
        <label
          className="mb-1 block text-sm font-medium"
          style={{ color: "var(--color-text-secondary)" }}
        >
          Bio
        </label>
        <textarea
          value={bio}
          onChange={(e) => setBio(e.target.value)}
          maxLength={500}
          rows={3}
          className={inputClass}
        />
        <p
          className="mt-1 text-right text-xs"
          style={{ color: "var(--color-text-tertiary)" }}
        >
          {bio.length} / 500
        </p>
      </div>
      <div>
        <label
          className="mb-1 block text-sm font-medium"
          style={{ color: "var(--color-text-secondary)" }}
        >
          Avatar URL
        </label>
        <input
          value={avatarUrl}
          onChange={(e) => setAvatarUrl(e.target.value)}
          type="url"
          className={inputClass}
        />
      </div>
      <div className="flex gap-2">
        <button
          type="submit"
          disabled={saving || !displayName.trim()}
          className="btn btn-primary rounded-md px-4 py-2 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
        >
          {saving ? "Saving..." : "Save"}
        </button>
        <button
          type="button"
          onClick={() => setEditing(false)}
          className="btn btn-quiet rounded-md px-4 py-2 text-sm"
        >
          Cancel
        </button>
      </div>
    </form>
  );
}
