export interface Post {
  id: string;
  author_id: string;
  body: string;
  image_path: string;
  status: string;
  created_at: string;
  edited_at: string | null;
}

export interface User {
  id: string;
  display_name: string;
  bio: string;
  avatar_url: string;
  trust_score: number;
  role: string;
  is_active: boolean;
  joined_at: string;
}

export interface ReactionCount {
  reaction_type: string;
  count: number;
}

export interface FeedResponse {
  posts: Post[];
  next_cursor?: string;
}

export interface CreatePostRequest {
  body: string;
  image_path?: string;
}

export interface Vouch {
  id: string;
  voucher_id: string;
  vouchee_id: string;
  status: string;
  created_at: string;
}

export interface VouchesResponse {
  received: Vouch[];
  given: Vouch[];
}

export interface UserPostsResponse {
  posts: Post[];
}

export interface UpdateProfileRequest {
  display_name: string;
  bio: string;
  avatar_url: string;
}

export interface ApiError {
  error: string;
  status: number;
}

export interface TownStats {
  total_users: number;
  posts_today: number;
  active_moderators: number;
  pending_users: number;
}

export interface CouncilVote {
  id: string;
  proposal_id: string;
  voter_id: string;
  vote: "approve" | "reject";
  created_at: string;
}

export interface ProposalSummary {
  proposal_id: string;
  approve_count: number;
  reject_count: number;
  total_council: number;
  status: "pending" | "approved" | "rejected";
  votes: CouncilVote[];
}

export interface PendingUsersResponse {
  users: User[];
}

export interface ProposalsResponse {
  proposals: ProposalSummary[];
}

export interface Report {
  id: string;
  reporter_id: string;
  post_id: string;
  reason: string;
  status: string;
  created_at: string;
}

export interface ModerationQueueResponse {
  reports: Report[];
}

export interface TrustPenalty {
  id: string;
  user_id: string;
  moderation_action_id: string;
  penalty_amount: number;
  hop_depth: number;
  created_at: string;
  decays_at: string | null;
}

export interface ModerationAction {
  id: string;
  target_user_id: string;
  moderator_id: string;
  action: string;
  severity: number;
  reason: string;
  duration: number | null;
  created_at: string;
  expires_at: string | null;
}

export interface ActionHistoryEntry {
  action: ModerationAction;
  penalties: TrustPenalty[];
}

export interface ActionHistoryResponse {
  actions: ActionHistoryEntry[];
}

export interface TakeActionRequest {
  target_user_id: string;
  action_type: string;
  severity: number;
  reason: string;
  duration_seconds?: number;
}
