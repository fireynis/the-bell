# User Guide

## Getting Started

### Registration

1. Navigate to the Bell's login page (`/auth/login`)
2. Click the registration link to create an account (`/auth/registration`)
3. Complete the Ory Kratos registration flow (email + password)
4. On first API request after registration, a local profile is auto-created with:
   - Role: `pending`
   - Trust score: 50
   - Active: yes

### Becoming a Member

As a `pending` user you cannot post or vouch. To become a `member`:

- **During bootstrap mode**: A council member can approve you directly from the admin dashboard
- **After bootstrap mode**: An existing member or moderator with trust >= 60 must vouch for you. Your first vouch automatically promotes you to `member`

## The Trust System

Every user has a **trust score** from 0 to 100. The score is a weighted composite of four components:

| Component | Weight | Description |
|-----------|--------|-------------|
| Tenure | 15% | How long you have been a member (0 at join, 100 at 365 days) |
| Activity | 20% | Recent posting and reactions received (90-day window). Posts contribute 50% (cap: 90 posts) and reactions received contribute 50% (cap: 270 reactions) |
| Voucher | 35% | Number of active vouches you have received and the average trust of your vouchers. Each vouch adds 15 points to a base (capped at 100), scaled by voucher trust health |
| Moderation | 30% | Starts at 100, reduced by active penalties. Penalties decay linearly over time based on severity |

### Trust Thresholds

| Threshold | Score | Effect |
|-----------|-------|--------|
| Posting | 30 | You must have trust >= 30 to create posts |
| Vouching | 60 | You must have trust >= 60 to vouch for others |
| Promotion | 85 | Eligible for automatic promotion to moderator (additional criteria apply) |
| Demotion | 70 | Trust below 70 for 30 consecutive days triggers automatic demotion |

## Posting

### Creating Posts

Navigate to `/compose` or click "New Post" from the feed. Posts support:

- **Text**: Up to 1,000 characters
- **Images**: Optional image upload (JPEG, PNG, or WebP, max 5 MB). Use multipart/form-data with a `body` field and an `image` file field

Requirements:
- You must be a `member`, `moderator`, or `council` (not `pending` or `banned`)
- Your account must be active (not suspended)
- Trust score >= 30
- Rate limit: 10 posts per hour (when Redis is enabled)

### Editing Posts

You can edit a post's text within **15 minutes** of creation. After that window closes, the post is locked. Only the author can edit their own posts.

### Deleting Posts

You can delete your own posts at any time. Deleted posts are marked as `removed_by_author` and no longer appear in the feed.

### Feed

The home feed (`/`) shows all visible posts in reverse chronological order. The feed uses cursor-based pagination -- scroll down and more posts load automatically. The default page size is 20 posts, with a maximum of 100.

## Vouching

### How It Works

Vouching is The Bell's trust mechanism. When you vouch for someone, you are saying "I trust this person to participate constructively." Your reputation is linked to theirs -- if they receive moderation actions, your trust score is also affected through penalty propagation.

### Giving a Vouch

To vouch for someone, you must:

- Have a trust score >= 60
- Be an active `member`, `moderator`, or `council`
- Not have already vouched for the same person
- Not be creating a cycle in the trust graph (A vouches for B, B vouches for A)
- Not exceed the daily limit of 3 vouches

### What Vouching Does

- If the recipient is `pending`, they are immediately promoted to `member`
- The vouch creates an edge in the trust graph (powered by Apache AGE)
- Your voucher score component improves the recipient's trust score
- If the recipient later receives moderation penalties, the penalty propagates to you (decaying by distance)

### Revoking a Vouch

Vouches can be revoked by:

- The original voucher (the person who gave the vouch)
- A moderator or council member

Revoking a vouch removes the trust graph edge and may affect the vouchee's trust score.

## Roles

The Bell has five roles, in ascending order of privilege:

| Role | Can Post | Can Vouch | Can Moderate | Can Administer |
|------|----------|-----------|--------------|----------------|
| `banned` | No | No | No | No |
| `pending` | No | No | No | No |
| `member` | Yes (trust >= 30) | Yes (trust >= 60) | No | No |
| `moderator` | Yes | Yes | Yes | No |
| `council` | Yes | Yes | Yes | Yes |

### Automatic Promotion (Member to Moderator)

A member is automatically promoted to moderator when all of the following are true:

- Trust score >= 85
- Has been a member for at least 90 days
- Has received at least 2 vouches from moderators or council members

Promotion checks run when `bell check-roles` is executed (typically via a daily cron job).

### Automatic Demotion

A user is automatically demoted when their trust score falls below 70 for 30 consecutive days:

- Moderator is demoted to member
- Member is demoted to pending

The demotion clock resets after each demotion.

### Council

Council members are never automatically promoted or demoted. They are set up during initial bootstrap (`bell setup`) and can only be changed manually. Council members have access to the admin dashboard (`/admin`), which shows:

- Town statistics (total users, posts today, active moderators, pending users)
- Pending user approval (during bootstrap mode)
- Council proposal voting

## Moderation

### Reporting a Post

Any active member can report a post they find problematic:

1. Click the report option on the post
2. Provide a reason (up to 1,000 characters)

Limits:
- You cannot report your own posts
- You can only report each post once
- Maximum 5 reports per hour

### What Moderators Can Do

Moderators and council members can access the moderation queue, which shows all pending reports. From there they can:

**Review Reports**: Mark reports as `reviewed` or `dismissed`.

**Take Moderation Actions** against users:

| Action | Severity | Direct Penalty | Decay | Effect |
|--------|----------|---------------|-------|--------|
| Warn (minor) | 1 | -5 points | 90 days | No immediate restriction |
| Warn (moderate) | 2 | -10 points | 180 days | No immediate restriction |
| Mute | 3 | -25 points | 270 days | Trust forced below posting threshold (< 30) |
| Suspend | 4 | -40 points | 365 days | Account deactivated |
| Ban | 5 | -100 points | Permanent | Role set to `banned`, trust set to 0 |

### Trust Penalty Propagation

When a moderation action is taken, the trust penalty propagates through the vouch graph to the offender's vouchers:

| Severity | Graph Depth | Decay Factor |
|----------|-------------|--------------|
| 1 (minor) | 1 hop | 0.50 |
| 2 (moderate) | 1 hop | 0.70 |
| 3 (serious) | 2 hops | 0.60 |
| 4 (severe) | 2 hops | 0.70 |
| 5 (ban) | 3 hops | 0.75 |

For example, if User A is banned (severity 5, penalty 100 points):
- User B (who vouched for A) receives 100 * 0.75 = 75 points penalty at depth 1
- User C (who vouched for B) receives 100 * 0.75^2 = 56.25 points penalty at depth 2
- User D (who vouched for C) receives 100 * 0.75^3 = 42.19 points penalty at depth 3

This incentivizes careful vouching -- vouching for bad actors has consequences.

### Moderation Action History

Moderators can view the full moderation history for any user, including all actions taken and the trust penalties that resulted. Council members can additionally view a moderator's action history (actions they have taken) for audit purposes.

## Council Voting

Council members can vote on proposals from the admin dashboard. Each proposal requires a simple majority of council members to pass:

- Vote options: `approve` or `reject`
- Each council member can vote once per proposal
- A proposal is approved when approve votes > total council / 2
- A proposal is rejected when reject votes > total council / 2

## Managing Your Profile

Visit `/profile` to see your profile, including:

- Display name, bio, and avatar
- Trust score with a visual bar
- Your role and join date
- Your posts and vouches (received and given)

Click "Edit profile" to update your display name (required, max 100 characters), bio (max 500 characters), and avatar URL.

Account settings (email, password) are managed through Kratos at `/auth/settings`.
