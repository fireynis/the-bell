import { useCallback, useEffect, useState } from "react";
import { useParams } from "react-router";
import { userApi, vouchApi } from "../api/client";
import { useAuth } from "../context/AuthContext";
import type { ApiError, Post, User, VouchesResponse } from "../api/types";
import Avatar from "../components/Avatar";
import ErrorBanner from "../components/ErrorBanner";
import OwnModerationHistory from "../components/OwnModerationHistory";
import OwnMuteNotice from "../components/OwnMuteNotice";
import PostCard from "../components/PostCard";
import RoleBadge from "../components/RoleBadge";
import Spinner from "../components/Spinner";
import TrustBar from "../components/TrustBar";
import EditProfileForm from "./profile/EditProfileForm";
import VouchList from "./profile/VouchList";
import VouchAction from "./profile/VouchAction";
import { vouchingBlockReason } from "../lib/gating";
import { replacePost, withoutPost } from "../lib/post";

/**
 * "history" is the member's own moderation record, and it is offered on their
 * own profile and nowhere else — the endpoint behind it answers only about the
 * caller, so there is nothing it could show on somebody else's page.
 */
type Tab = "posts" | "vouches" | "history";

/** How many of a profile's posts to show; the page does not paginate them. */
const POST_LIMIT = 50;

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
  // The viewer's own vouches, needed on someone else's profile to answer
  // "have I already vouched for them" and "have I any vouches left today".
  const [viewerVouches, setViewerVouches] = useState<VouchesResponse | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<Tab>("posts");

  const viewerId = viewer?.id;

  const fetchProfile = useCallback(async () => {
    setLoading(true);
    setError(null);

    try {
      const userData = isOwnProfile ? await userApi.getProfile() : await userApi.getById(userId!);
      setUser(userData);

      const resolvedId = userData.id;
      const needsViewerVouches = !!viewerId && viewerId !== resolvedId;

      const [postsData, vouchData, viewerVouchData] = await Promise.all([
        userApi.listPosts(resolvedId, POST_LIMIT),
        vouchApi.listForUser(resolvedId),
        needsViewerVouches ? vouchApi.listForUser(viewerId) : Promise.resolve(null),
      ]);

      setPosts(postsData.posts ?? []);
      setVouches(vouchData);
      setViewerVouches(viewerVouchData);
    } catch (err) {
      const apiErr = err as ApiError;
      setError(apiErr.error ?? "Failed to load profile.");
    } finally {
      setLoading(false);
    }
  }, [isOwnProfile, userId, viewerId]);

  useEffect(() => {
    fetchProfile();
  }, [fetchProfile]);

  if (loading) {
    return (
      <div className="py-5">
        <div className="flex justify-center py-12">
          <Spinner size="lg" />
        </div>
      </div>
    );
  }

  if (error || !user) {
    return (
      <div className="py-5">
        <ErrorBanner
          message={error ?? "User not found."}
          onRetry={fetchProfile}
        />
      </div>
    );
  }

  // "My history" exists only on the member's own profile, and the selected tab
  // survives navigating from there to a neighbour's page. Falling back leaves
  // them on a real tab rather than an empty panel under a row of buttons none
  // of which looks selected.
  const currentTab: Tab = activeTab === "history" && !isOwnProfile ? "posts" : activeTab;

  // Every tab shares one style; the active one is distinguished by the inline
  // colours applied at each call site.
  const tabClasses = "px-4 py-2 text-sm font-medium border-b-2";

  const tabStyle = (tab: Tab): React.CSSProperties =>
    currentTab === tab
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
      <div>
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

          {/*
            The member's own mute, and any mute a moderator lifted early. Only
            ever on their own profile: a mute is between the member and the
            moderators, and the server sends these fields nowhere else.
          */}
          {isOwnProfile && <OwnMuteNotice user={user} />}

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

          {!isOwnProfile && (
            <div
              className="mt-4 border-t pt-4"
              style={{ borderColor: "var(--color-border-light)" }}
            >
              <VouchAction
                viewer={viewer}
                target={user}
                received={vouches?.received ?? []}
                given={viewerVouches?.given ?? []}
                onChanged={fetchProfile}
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
          {/*
            No count on this one. A number beside "My history" would announce
            how many times a member has been moderated every time they open
            their own profile, including the zero that most people will have —
            and a tab reading "My history (0)" invites a look at something that
            has nothing in it. The count also cannot be known without making
            the request, which the tab exists to defer.
          */}
          {isOwnProfile && (
            <button
              className={tabClasses}
              style={tabStyle("history")}
              onClick={() => setActiveTab("history")}
            >
              My history
            </button>
          )}
        </div>

        {/* Tab Content */}
        <div className="mt-4">
          {currentTab === "posts" && (
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
                    <PostCard
                      key={post.id}
                      post={post}
                      onUpdated={(updated) => setPosts((prev) => replacePost(prev, updated))}
                      onRemoved={(postId) => setPosts((prev) => withoutPost(prev, postId))}
                    />
                  ))}
                </div>
              )}
            </div>
          )}

          {currentTab === "vouches" && vouches && (
            <div className="space-y-6">
              <VouchList
                direction="received"
                vouches={vouches.received}
                viewer={viewer}
                ownerName={user.display_name}
                onRevoked={fetchProfile}
              />
              <VouchList
                direction="given"
                vouches={vouches.given}
                viewer={viewer}
                ownerName={user.display_name}
                onRevoked={fetchProfile}
              />
            </div>
          )}

          {/*
            Mounted only while the tab is open, which is what keeps the read
            off every profile load: a member who never opens this never asks
            the server for it. Guarded on isOwnProfile as well as on the tab,
            so no future change to how tabs are selected can put somebody
            else's page in front of this component.
          */}
          {currentTab === "history" && isOwnProfile && <OwnModerationHistory />}
        </div>
      </div>
    </div>
  );
}
