import { useCallback, useEffect, useState } from "react";
import { useParams } from "react-router";
import { api } from "../api/client";
import { useAuth } from "../context/AuthContext";
import type {
  ApiError,
  Post,
  User,
  UserPostsResponse,
  VouchesResponse,
} from "../api/types";
import Avatar from "../components/Avatar";
import ErrorBanner from "../components/ErrorBanner";
import PostCard from "../components/PostCard";
import RoleBadge from "../components/RoleBadge";
import Spinner from "../components/Spinner";
import TrustBar from "../components/TrustBar";
import EditProfileForm from "./profile/EditProfileForm";
import VouchList from "./profile/VouchList";
import { vouchingBlockReason } from "../lib/gating";

type Tab = "posts" | "vouches";

export default function Profile() {
  const { userId } = useParams<{ userId: string }>();
  const { user: viewer } = useAuth();
  const isOwnProfile = !userId;

  // Derived from the signed-in viewer, not the profile being read: it answers
  // "can I vouch", which is only shown on the viewer's own profile.
  const vouchBlock = vouchingBlockReason(viewer);

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

  // Both tabs share one style; the active tab is distinguished by the inline
  // colours applied at each call site.
  const tabClasses = "px-4 py-2 text-sm font-medium border-b-2";

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
              {/*
                Vouching is the platform's core mechanic, so where the viewer
                stands against its threshold is worth saying plainly on their
                own profile rather than leaving them to infer it from a score.
              */}
              <p
                className="mt-3 text-xs"
                style={{ color: "var(--color-text-tertiary)" }}
              >
                {vouchBlock ?? "You have enough trust to vouch for other members."}
              </p>
            </div>
          )}
        </div>

        {/* Tabs */}
        <div
          className="mt-6 flex border-b"
          style={{ borderColor: "var(--color-border-light)" }}
        >
          <button
            className={tabClasses}
            style={tabStyle("posts")}
            onClick={() => setActiveTab("posts")}
          >
            Posts ({posts.length})
          </button>
          <button
            className={tabClasses}
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
              <VouchList direction="received" vouches={vouches.received} />
              <VouchList direction="given" vouches={vouches.given} />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
