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

You can delete your own posts at any time. Deleted posts are marked as `removed_by_author` and no longer appear in the feed or on your profile.

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

Revoking a vouch removes the trust graph edge and may affect the vouchee's trust
score.

**Revoking your own vouch costs you 3 trust points for 30 days.** Withdrawing an
endorsement is meant to cost something — otherwise vouching and revoking in a
loop would be a free way to game the graph. The size is deliberate: 3 points
against a 100-point scale leaves a voucher who was at the threshold still above
it, so removing a vouch you have come to regret never costs you the ability to
vouch at all.

**A moderator or council member revoking somebody else's vouch pays nothing.**
They are doing the job, and charging them would discourage exactly the cleanup
the trust graph depends on. If a voucher's judgement is the actual problem, the
remedy is a moderation action against them, which carries its own penalty.

Unlike a moderation penalty, the revocation penalty does not propagate: it stops
with the voucher and never reaches the people who vouched for *them*.

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

**Check and Lift Mutes or Suspensions**: See whether a member is currently
muted or suspended and when the restriction ends, and end it early if it was
applied in error or the member appeals successfully. Check the live status
rather than reading the audit trail: a past mute or suspension entry keeps its
original end time forever and will keep suggesting a restriction that may
already be over. A moderator cannot lift a mute or suspension on themselves.

**Remove a Post**: Take a single post down with a reason. The post stops
appearing in the feed and on its author's profile, exactly as an author's own
deletion does. The difference is what is recorded: a moderator removal stores
both the reason and which moderator acted. Neither is ever shown to the author
or to anyone else — they are for the audit trail.

A removed post is not merely hidden from lists. Requesting it directly returns
"not found" to everyone except its author and active moderators, and that
response is indistinguishable from a post that never existed.

Removing a post is separate from actioning its author. Taking a post down
carries no trust penalty on its own; if the author's behaviour warrants one,
that is a moderation action.

**Take Moderation Actions** against users:

| Action | Severity | Direct Penalty | Decay | Effect |
|--------|----------|---------------|-------|--------|
| Warn (minor) | 1 | -5 points | 90 days | No immediate restriction |
| Warn (moderate) | 2 | -10 points | 180 days | No immediate restriction |
| Mute | 3 | -25 points | 270 days | Cannot post until the mute expires. Trust score is not changed |
| Suspend | 4 | -40 points | 365 days | Account inactive until the suspension expires |
| Ban | 5 | -100 points | Permanent | Role set to `banned`, trust set to 0 |

A mute lasts exactly as long as the moderator set it for, and it is recorded
separately from the trust score rather than by forcing the score down. You can
see your own mute — and when it ends — on your profile. Moderators and council
members can see it too, since they are the other party to it; nobody else can,
and it appears nowhere on your public profile. Note that the trust penalty above
still applies and decays on its own schedule, so your score may stay reduced
after the mute itself has ended.

**Lifting a mute early.** A moderator or council member can end a mute before it
runs out — for one applied in error, or one they agree to shorten after you
appeal. Lifting is not a further punishment and is not recorded as one: it costs
nobody trust, propagates nothing through the vouch graph, and does not appear in
anyone's moderation history.

One consequence is worth knowing if you read your own history: the original mute
entry stays exactly as it was written, still showing the end time the moderator
first chose, with nothing marking it as lifted. That record is what was decided
at the time, not a live status. If your mute has been lifted you can simply
post again; your profile is the place that tells you whether a mute is still in
force.

Moderators cannot lift their own mutes.

**Lifting a suspension early.** Suspensions work the same way: they end on
their own at the time the moderator set, and a moderator or council member can
end one sooner. Like a lifted mute, an early release costs nobody trust,
propagates nothing, and is not filed as a moderation action. A suspension is
less visible to you than a mute while it runs — your account simply reads as
inactive — so if one is lifted early, that release appears on your own profile
beside any lifted mutes. Nobody else sees it, and moderators cannot lift their
own suspensions.

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
