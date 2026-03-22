import { useCallback, useEffect, useState } from "react";
import { Link, useParams } from "react-router";
import { api } from "../api/client";
import type {
  ApiError,
  Post,
  User,
  UserPostsResponse,
  VouchesResponse,
  Vouch,
} from "../api/types";
import Avatar from "../components/Avatar";
import ErrorBanner from "../components/ErrorBanner";
import PostCard from "../components/PostCard";
import RoleBadge from "../components/RoleBadge";
import Spinner from "../components/Spinner";
import TrustBar from "../components/TrustBar";

type Tab = "posts" | "vouches";

function EditProfileForm({
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
      const updated = await api.put<User>("/users/me", {
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

  const inputClass =
    "w-full rounded-md px-3 py-2 text-sm focus:outline-none focus:ring-1";
  const inputStyle = {
    border: "1px solid var(--color-border)",
  };

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
          style={inputStyle}
          onFocus={(e) =>
            (e.currentTarget.style.borderColor = "var(--color-primary)")
          }
          onBlur={(e) =>
            (e.currentTarget.style.borderColor = "var(--color-border)")
          }
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
          style={inputStyle}
          onFocus={(e) =>
            (e.currentTarget.style.borderColor = "var(--color-primary)")
          }
          onBlur={(e) =>
            (e.currentTarget.style.borderColor = "var(--color-border)")
          }
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
          style={inputStyle}
          onFocus={(e) =>
            (e.currentTarget.style.borderColor = "var(--color-primary)")
          }
          onBlur={(e) =>
            (e.currentTarget.style.borderColor = "var(--color-border)")
          }
        />
      </div>
      <div className="flex gap-2">
        <button
          type="submit"
          disabled={saving || !displayName.trim()}
          className="rounded-md px-4 py-2 text-sm font-medium disabled:cursor-not-allowed disabled:opacity-50"
          style={{
            backgroundColor: "var(--color-primary)",
            color: "var(--color-text-inverse)",
          }}
        >
          {saving ? "Saving..." : "Save"}
        </button>
        <button
          type="button"
          onClick={() => setEditing(false)}
          className="rounded-md px-4 py-2 text-sm"
          style={{
            backgroundColor: "var(--color-surface-tertiary)",
            color: "var(--color-text-secondary)",
          }}
        >
          Cancel
        </button>
      </div>
    </form>
  );
}

function VouchList({
  title,
  vouches,
}: {
  title: string;
  vouches: Vouch[];
}) {
  if (vouches.length === 0) {
    return (
      <div>
        <h3
          className="mb-2 text-sm font-medium"
          style={{ color: "var(--color-text-secondary)" }}
        >
          {title}
        </h3>
        <p className="text-sm" style={{ color: "var(--color-text-tertiary)" }}>
          None yet.
        </p>
      </div>
    );
  }

  return (
    <div>
      <h3
        className="mb-2 text-sm font-medium"
        style={{ color: "var(--color-text-secondary)" }}
      >
        {title}
      </h3>
      <ul className="space-y-2">
        {vouches.map((v) => (
          <li
            key={v.id}
            className="rounded-md p-3"
            style={{
              backgroundColor: "var(--color-surface)",
              boxShadow: "var(--shadow-sm)",
            }}
          >
            <div className="flex items-center justify-between">
              <Link
                to={`/profile/${title === "Received" ? v.voucher_id : v.vouchee_id}`}
                className="text-sm font-medium"
                style={{ color: "var(--color-primary)" }}
              >
                {(title === "Received" ? v.voucher_id : v.vouchee_id).slice(0, 8)}...
              </Link>
              <span
                className="text-xs"
                style={{ color: "var(--color-text-tertiary)" }}
              >
                {new Date(v.created_at).toLocaleDateString()}
              </span>
            </div>
          </li>
        ))}
      </ul>
    </div>
  );
}

export default function Profile() {
  const { userId } = useParams<{ userId: string }>();
  const isOwnProfile = !userId;

  const [user, setUser] = useState<User | null>(null);
  const [posts, setPosts] = useState<Post[]>([]);
  const [vouches, setVouches] = useState<VouchesResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<Tab>("posts");

  const apiPath = isOwnProfile ? "/users/me" : `/users/${userId}`;

  const fetchProfile = useCallback(async () => {
    setLoading(true);
    setError(null);

    try {
      const userData = await api.get<User>(apiPath);
      setUser(userData);

      const resolvedId = userData.id;

      const [postsData, vouchData] = await Promise.all([
        api.get<UserPostsResponse>(`/users/${resolvedId}/posts?limit=50`),
        api.get<VouchesResponse>(`/users/${resolvedId}/vouches`),
      ]);

      setPosts(postsData.posts ?? []);
      setVouches(vouchData);
    } catch (err) {
      const apiErr = err as ApiError;
      setError(apiErr.error ?? "Failed to load profile.");
    } finally {
      setLoading(false);
    }
  }, [apiPath]);

  useEffect(() => {
    fetchProfile();
  }, [fetchProfile]);

  if (loading) {
    return (
      <div className="mx-auto max-w-2xl p-4">
        <div className="flex justify-center py-12">
          <Spinner size="lg" />
        </div>
      </div>
    );
  }

  if (error || !user) {
    return (
      <div className="mx-auto max-w-2xl p-4">
        <ErrorBanner
          message={error ?? "User not found."}
          onRetry={fetchProfile}
        />
      </div>
    );
  }

  const tabClasses = (tab: Tab) => {
    const base = "px-4 py-2 text-sm font-medium border-b-2";
    return base;
  };

  const tabStyle = (tab: Tab): React.CSSProperties =>
    activeTab === tab
      ? {
          borderColor: "var(--color-primary)",
          color: "var(--color-primary)",
        }
      : {
          borderColor: "transparent",
          color: "var(--color-text-secondary)",
        };

  return (
    <div className="py-5">
      <div className="mx-auto max-w-2xl px-4">
        {/* Profile Header */}
        <div
          className="rounded-lg p-6"
          style={{
            backgroundColor: "var(--color-surface)",
            boxShadow: "var(--shadow-md)",
            borderRadius: "var(--radius-lg)",
          }}
        >
          <div className="flex items-start gap-4">
            <Avatar url={user.avatar_url} name={user.display_name} size="lg" />
            <div className="flex-1">
              <div className="flex items-center gap-2">
                <h1
                  className="text-xl font-bold"
                  style={{ color: "var(--color-text)" }}
                >
                  {user.display_name || user.id.slice(0, 8)}
                </h1>
                <RoleBadge role={user.role} />
              </div>
              {user.bio && (
                <p
                  className="mt-1 text-sm"
                  style={{ color: "var(--color-text-secondary)" }}
                >
                  {user.bio}
                </p>
              )}
              <div className="mt-3 flex flex-wrap items-center gap-4">
                <TrustBar score={user.trust_score} />
                <span
                  className="text-sm"
                  style={{ color: "var(--color-text-tertiary)" }}
                >
                  Joined {new Date(user.joined_at).toLocaleDateString()}
                </span>
              </div>
            </div>
          </div>

          {isOwnProfile && (
            <div
              className="mt-4 border-t pt-4"
              style={{ borderColor: "var(--color-border-light)" }}
            >
              <EditProfileForm
                user={user}
                onSave={(updated) => setUser(updated)}
              />
            </div>
          )}
        </div>

        {/* Tabs */}
        <div
          className="mt-6 flex border-b"
          style={{ borderColor: "var(--color-border-light)" }}
        >
          <button
            className={tabClasses("posts")}
            style={tabStyle("posts")}
            onClick={() => setActiveTab("posts")}
          >
            Posts ({posts.length})
          </button>
          <button
            className={tabClasses("vouches")}
            style={tabStyle("vouches")}
            onClick={() => setActiveTab("vouches")}
          >
            Vouches (
            {vouches
              ? vouches.received.length + vouches.given.length
              : 0}
            )
          </button>
        </div>

        {/* Tab Content */}
        <div className="mt-4">
          {activeTab === "posts" && (
            <div>
              {posts.length === 0 ? (
                <p
                  className="text-sm"
                  style={{ color: "var(--color-text-tertiary)" }}
                >
                  No posts yet.
                </p>
              ) : (
                <div className="flex flex-col gap-4">
                  {posts.map((post) => (
                    <PostCard key={post.id} post={post} />
                  ))}
                </div>
              )}
            </div>
          )}

          {activeTab === "vouches" && vouches && (
            <div className="space-y-6">
              <VouchList title="Received" vouches={vouches.received} />
              <VouchList title="Given" vouches={vouches.given} />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
