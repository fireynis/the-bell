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
import PostCard from "../components/PostCard";

type Tab = "posts" | "vouches";

function RoleBadge({ role }: { role: string }) {
  const colors: Record<string, string> = {
    council: "bg-purple-100 text-purple-800",
    moderator: "bg-blue-100 text-blue-800",
    member: "bg-green-100 text-green-800",
    pending: "bg-gray-100 text-gray-600",
    banned: "bg-red-100 text-red-800",
  };
  const colorClass = colors[role] ?? "bg-gray-100 text-gray-600";

  return (
    <span
      className={`inline-block rounded-full px-2.5 py-0.5 text-xs font-medium ${colorClass}`}
    >
      {role}
    </span>
  );
}

function TrustBar({ score }: { score: number }) {
  const clamped = Math.max(0, Math.min(100, score));
  let barColor = "bg-red-500";
  if (clamped >= 60) barColor = "bg-green-500";
  else if (clamped >= 30) barColor = "bg-yellow-500";

  return (
    <div className="flex items-center gap-2">
      <span className="text-sm font-medium text-gray-700">Trust</span>
      <div className="h-2 w-24 rounded-full bg-gray-200">
        <div
          className={`h-2 rounded-full ${barColor}`}
          style={{ width: `${clamped}%` }}
        />
      </div>
      <span className="text-sm text-gray-600">{Math.round(clamped)}</span>
    </div>
  );
}

function Avatar({
  url,
  name,
  size = "lg",
}: {
  url: string;
  name: string;
  size?: "sm" | "lg";
}) {
  const sizeClass = size === "lg" ? "h-16 w-16 text-2xl" : "h-10 w-10 text-base";

  if (url) {
    return (
      <img
        src={url}
        alt={name}
        className={`${sizeClass} rounded-full object-cover`}
      />
    );
  }

  const initial = (name || "?").charAt(0).toUpperCase();
  return (
    <div
      className={`${sizeClass} flex items-center justify-center rounded-full bg-indigo-100 font-semibold text-indigo-600`}
    >
      {initial}
    </div>
  );
}

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
        className="text-sm text-indigo-600 hover:text-indigo-500"
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

  return (
    <form onSubmit={handleSubmit} className="mt-4 space-y-3">
      {error && (
        <div className="rounded-md bg-red-50 p-2 text-sm text-red-700">
          {error}
        </div>
      )}
      <div>
        <label className="mb-1 block text-sm font-medium text-gray-700">
          Display name
        </label>
        <input
          value={displayName}
          onChange={(e) => setDisplayName(e.target.value)}
          maxLength={100}
          required
          className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
        />
      </div>
      <div>
        <label className="mb-1 block text-sm font-medium text-gray-700">
          Bio
        </label>
        <textarea
          value={bio}
          onChange={(e) => setBio(e.target.value)}
          maxLength={500}
          rows={3}
          className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
        />
        <p className="mt-1 text-right text-xs text-gray-400">
          {bio.length} / 500
        </p>
      </div>
      <div>
        <label className="mb-1 block text-sm font-medium text-gray-700">
          Avatar URL
        </label>
        <input
          value={avatarUrl}
          onChange={(e) => setAvatarUrl(e.target.value)}
          type="url"
          className="w-full rounded-md border border-gray-300 px-3 py-2 text-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
        />
      </div>
      <div className="flex gap-2">
        <button
          type="submit"
          disabled={saving || !displayName.trim()}
          className="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-500 disabled:cursor-not-allowed disabled:opacity-50"
        >
          {saving ? "Saving..." : "Save"}
        </button>
        <button
          type="button"
          onClick={() => setEditing(false)}
          className="rounded-md bg-gray-100 px-4 py-2 text-sm text-gray-700 hover:bg-gray-200"
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
        <h3 className="mb-2 text-sm font-medium text-gray-700">{title}</h3>
        <p className="text-sm text-gray-500">None yet.</p>
      </div>
    );
  }

  return (
    <div>
      <h3 className="mb-2 text-sm font-medium text-gray-700">{title}</h3>
      <ul className="space-y-2">
        {vouches.map((v) => (
          <li key={v.id} className="rounded-md bg-white p-3 shadow-sm">
            <div className="flex items-center justify-between">
              <Link
                to={`/profile/${title === "Received" ? v.voucher_id : v.vouchee_id}`}
                className="text-sm font-medium text-indigo-600 hover:text-indigo-500"
              >
                {(title === "Received" ? v.voucher_id : v.vouchee_id).slice(0, 8)}...
              </Link>
              <span className="text-xs text-gray-500">
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
          <div className="h-8 w-8 animate-spin rounded-full border-2 border-gray-300 border-t-indigo-600" />
        </div>
      </div>
    );
  }

  if (error || !user) {
    return (
      <div className="mx-auto max-w-2xl p-4">
        <div className="mb-4">
          <Link to="/" className="text-sm text-indigo-600 hover:text-indigo-500">
            &larr; Back to feed
          </Link>
        </div>
        <div className="rounded-md bg-red-50 p-4 text-sm text-red-700">
          {error ?? "User not found."}
          <button
            onClick={fetchProfile}
            className="ml-2 font-medium underline"
          >
            Retry
          </button>
        </div>
      </div>
    );
  }

  const tabClasses = (tab: Tab) =>
    `px-4 py-2 text-sm font-medium border-b-2 ${
      activeTab === tab
        ? "border-indigo-600 text-indigo-600"
        : "border-transparent text-gray-500 hover:text-gray-700 hover:border-gray-300"
    }`;

  return (
    <div className="mx-auto max-w-2xl p-4">
      <div className="mb-6">
        <Link to="/" className="text-sm text-indigo-600 hover:text-indigo-500">
          &larr; Back to feed
        </Link>
      </div>

      {/* Profile Header */}
      <div className="rounded-lg bg-white p-6 shadow">
        <div className="flex items-start gap-4">
          <Avatar url={user.avatar_url} name={user.display_name} />
          <div className="flex-1">
            <div className="flex items-center gap-2">
              <h1 className="text-xl font-bold text-gray-900">
                {user.display_name || user.id.slice(0, 8)}
              </h1>
              <RoleBadge role={user.role} />
            </div>
            {user.bio && (
              <p className="mt-1 text-sm text-gray-600">{user.bio}</p>
            )}
            <div className="mt-3 flex flex-wrap items-center gap-4">
              <TrustBar score={user.trust_score} />
              <span className="text-sm text-gray-500">
                Joined {new Date(user.joined_at).toLocaleDateString()}
              </span>
            </div>
          </div>
        </div>

        {isOwnProfile && (
          <div className="mt-4 border-t border-gray-100 pt-4">
            <EditProfileForm
              user={user}
              onSave={(updated) => setUser(updated)}
            />
          </div>
        )}
      </div>

      {/* Tabs */}
      <div className="mt-6 flex border-b border-gray-200">
        <button className={tabClasses("posts")} onClick={() => setActiveTab("posts")}>
          Posts ({posts.length})
        </button>
        <button
          className={tabClasses("vouches")}
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
              <p className="text-sm text-gray-500">No posts yet.</p>
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
  );
}
