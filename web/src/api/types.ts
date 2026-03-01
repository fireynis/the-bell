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

export interface ApiError {
  error: string;
  status: number;
}
