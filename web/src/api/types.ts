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

export interface Report {
  id: string;
  reporter_id: string;
  post_id: string;
  reason: string;
  status: string;
  created_at: string;
}

export interface ModerationAction {
  id: string;
  target_user_id: string;
  moderator_id: string;
  action: string;
  severity: number;
  reason: string;
  duration?: number;
  created_at: string;
  expires_at?: string;
}

export interface TrustPenalty {
  id: string;
  user_id: string;
  moderation_action_id: string;
  penalty_amount: number;
  hop_depth: number;
  created_at: string;
  decays_at?: string;
}

export interface ActionHistoryEntry {
  action: ModerationAction;
  penalties: TrustPenalty[];
}

export interface TakeActionRequest {
  target_user_id: string;
  action_type: string;
  severity: number;
  reason: string;
  duration_seconds?: number;
}

export interface TakeActionResult {
  action: ModerationAction;
  penalties: TrustPenalty[];
}

export interface QueueResponse {
  reports: Report[];
}

export interface ActionsResponse {
  actions: ActionHistoryEntry[];
}

export interface ApiError {
  error: string;
  status: number;
}
