# User Guide

## Getting Started

### Joining, by invitation

Most towns on The Bell are **invitation only**. You cannot sign yourself up;
somebody who already lives there invites you, and that invitation is also their
vouch for you. That is the whole design — membership runs on people who know
people, not on anyone who finds the address.

What it looks like from your side:

1. A neighbour invites you and you get an email: who invited you, whatever they
   wanted to say to you, and a link.
2. Follow the link. The sign-up page greets you by the address you were invited
   at and asks for a password.
3. **Register with that same address.** The invitation is for one address and
   will not accept another — if you sign up with a different one, you will be
   turned away and have to start again with the right one.
4. That is it. You are a **member** straight away, because accepting the
   invitation records your neighbour vouching for you. No waiting, no queue.

Two things worth knowing about the link:

- **It expires after 14 days.** If yours has run out, ask whoever invited you to
  send another — they can, as soon as the old one lapses.
- **It is yours.** Forwarding it to somebody else does not work: the address has
  to match. If you were expecting an invitation and never got one, check your
  spam folder before asking again, since a second one cannot be sent while the
  first is still live.

If your neighbour's own standing has changed since they invited you — if they
have been suspended, say, or their trust has dropped below the vouching
threshold — the invitation still lets you register, but you arrive as a
`pending` resident instead of a member. The routes below are then how you get
the rest of the way, and they are open to you as they are to anybody.

### Joining a town with open sign-up

Some towns leave registration open. There you can create an account yourself:

1. Navigate to the Bell's login page (`/auth/login`)
2. Click the registration link to create an account (`/auth/registration`)
3. Complete the Ory Kratos registration flow (email + password)
4. On first API request after registration, a local profile is auto-created with:
   - Role: `pending`
   - Trust score: 50
   - Active: yes

### Becoming a Member

If you arrived by invitation you are already a member and can skip this. As a
`pending` user you cannot post or vouch. To become a `member`:

- **By invitation**: accepting one makes you a member the moment you sign in, as
  above
- **During bootstrap mode**: A council member can approve you directly from the admin dashboard
- **After bootstrap mode**: An existing member or moderator with trust >= 60 must vouch for you. Your first vouch automatically promotes you to `member`

### Inviting somebody yourself

Once you are a member with trust >= 60 — the same bar as vouching — you can
invite people. Give an email address and, if you like, a note; they get the
message and you get a link you can also pass on by hand.

An invitation **is** a vouch, so it spends the same allowance: **three a day,
invitations and vouches together**. Two invitations and one vouch is your day
spent. Nothing is charged again when the person accepts, however long they take.
(Council members are not rationed.)

Your invitations are on your own profile: who you invited, whether they have
accepted, and when each one runs out. You can withdraw one that is still open,
which also frees that address so you can send a corrected invitation to a
mistyped one. Nobody else can see your list, and the link itself is shown only
once, when you create it — if you lose it, withdraw the invitation and send a
new one.

Invite people the way you would vouch for them: because you know who they are.
Their conduct affects your trust score the same way, since accepting is your
endorsement of them.

### Saying where you live

While you are waiting, you can tell the council where in town you live. It is
one free-text field on your profile, and it is optional.

It helps because the council is deciding whether you are a neighbour, and a
street they recognise is often the thing that settles it — especially if nobody
has vouched for you yet and your name alone means nothing to them. Write it
however a neighbour would say it. "12 Mill Lane" works; so does "the blue house
behind the old mill". Up to 300 characters.

Two things worth knowing:

- **Nobody verifies it.** The Bell does not check your address against anything.
  It shows the council what you wrote and records who approved you on the
  strength of it. What your town does with a claim it cannot place — ask around,
  ask you, wait for a vouch — is up to your town.
- **Only you and the reviewing council see it.** It is not on your public
  profile, not in the member directory, and not visible to other residents. Your
  own profile shows it back to you, so you can see what you said and change it or
  clear it at any time.

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
| Demotion (moderator) | 70 | A moderator below 70 for 30 consecutive days is demoted to member |
| Demotion (member) | 35 | A member below 35 for 30 consecutive days is demoted to pending, once their account is at least 90 days old |

## Posting

### Creating Posts

Navigate to `/compose` or click "New Post" from the feed. Posts support:

- **Text**: Up to 1,000 characters
- **Images**: Optional image upload (JPEG, PNG, or WebP, max 5 MB). Use multipart/form-data with a `body` field and an `image` file field
- **Image descriptions**: Optional, up to 500 characters, in the `alt_text` field alongside the image

### Describing your image

When you attach an image, the composer asks you to describe it. That
description is what a neighbour using a screen reader hears in place of the
picture -- without one, the image is announced as nothing at all and your post
reaches them as text with a silent gap in it.

It takes a sentence. Say what someone would need to know if they were standing
next to you and could not see the screen: "The bandstand in Wilson Park,
freshly painted green" rather than "photo" or "image of the park". If words in
the picture matter -- a notice, a date on a poster, a road-closure sign --
include them, because nothing else on the page carries them.

You are never blocked from posting without one. It is a courtesy to the people
in town who need it, and the composer keeps asking because it is easy to forget
that someone is reading with their ears.

Requirements:
- You must be a `member`, `moderator`, or `council` (not `pending` or `banned`)
- Your account must be active (not suspended)
- Trust score >= 30
- Rate limit: 10 posts per hour (when Redis is enabled)

### Editing Posts

You can edit a post's text within **15 minutes** of creation. After that window closes, the post is locked. Only the author can edit their own posts.

If the post has an image, its description can be edited in the same window and
in the same dialog -- so a description you skipped in a hurry can still be added,
as long as you get to it within the fifteen minutes. The image itself cannot be
changed or removed; delete the post and post again instead.

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
- Not exceed the daily limit of 3, **shared with invitations** — an invitation
  is a vouch made in advance, so the two draw on one allowance. Council members
  are exempt from it

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

Each role has its own demotion bar, and a user is demoted after sitting below
it for 30 consecutive days:

- A moderator below **70** is demoted to member
- A member below **35** is demoted to pending

**New members are not judged for their first 90 days.** Two parts of your trust
score can only go up with time — tenure counts days since you joined, and
activity is measured over a rolling 90-day window — so a member who was vouched
in last month scores low no matter how well they behave. A quiet newcomer with
one vouch from a mid-trust neighbour computes to around 34, which is under the
bar on their very first day. Applying the bar to them would demote healthy new
residents a month after they arrived, so the member bar starts applying once the
account is 90 days old, by which point tenure alone has lifted the score clear.
The clock that counts consecutive days below the bar does not run during those
90 days either, so nothing is banked against a new member.

Moderators get no such grace: becoming a moderator already requires 90 days as
a member, so every moderator is past that point when the role is granted.

The bars differ on purpose. A healthy, quietly-participating member computes to
roughly 50, so the member bar sits well beneath that: a served suspension or
being a few hops from someone else's ban is survivable, and only genuinely
collapsed trust crosses it. Moderators are held to the standing the role was
granted for. Council, pending and banned users are never on a demotion clock,
and recovering above your bar resets it.

A demotion also clears the clock, and landing on a role with no bar clears it
just the same. That matters for the member who drops to pending: the days they
spent below 35 are not still counting, so a neighbour vouching them back up to
member does not put them one sweep away from being demoted all over again.

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

**Your own moderation history.** Open your profile and choose **My history** to
see everything moderation has done to your account: what the action was, the
reason the moderator wrote, when it happened, when any restriction ends, and how
many trust points it cost you — along with the month that cost will have faded
away, since most penalties decay. Most people will open it once and find
"Nothing here", which is the normal state of an account. It is readable even
while you are suspended or banned; that is when you most need it.

Two things are deliberately not there. It does not say **which** moderator
acted: a decision belongs to the moderation team, and if you disagree with one,
that is who to take it up with. And it shows only what the action cost **you** —
a penalty also reaches the people who vouched for you, but what it cost them is
their business, not yours. Nobody else can see your history, and you cannot see
anybody else's.

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

## The Council

The council is the town's standing body. Its members approve residents during
bootstrap mode, change town configuration, and issue the heaviest moderation
sanctions.

**The council is not fixed at setup, and a seat on it is not permanent.** It
begins with whoever the operator named when the town was created, and after that
it changes the same way anything else about the town changes: the council votes.

### Proposals

Any council member can raise a proposal from the admin dashboard. There are
three kinds, and each one *does* something the moment it passes — there is no
separate step where somebody applies the result:

| Proposal | What it asks | What passing it does |
|----------|--------------|----------------------|
| Promote to council | That a moderator join the council | They become a council member |
| Remove from council | That a council member step down | They return to being a `member` |
| Re-enter bootstrap mode | That the town go back to admitting residents by council approval | Bootstrap mode is switched back on |

Every proposal carries a rationale — the case the proposer is making to their
colleagues — and that rationale is the record of why a seat changed hands.

### How the vote works

A proposal passes on a simple majority: strictly more than half of the council
members entitled to vote on it. It is decided the moment either side reaches
that majority, so a proposal does not wait for everyone.

Each council member votes once, approve or reject. Changing your mind afterwards
is not possible; abstaining is, and you do it by not voting.

**On a removal, the person being removed does not vote and is not counted.** The
majority is taken over the rest of the council. Nobody gets a say in whether
they keep their own seat.

Only a moderator can be proposed for the council — the moderator role is where
the standing for it is earned — and the council cannot vote itself out of
existence: a removal that would leave no council members is refused.

### Re-entering bootstrap mode

Bootstrap mode is how a new town admits its first residents, by council approval
rather than by vouching, and it ends automatically at 20 active members. That
used to be a one-way door: a town that grew past 20 and then shrank had no way
back to the only mechanism that lets people in without a vouch.

A council vote can now reopen it. The one restriction is that the town has to be
below 20 active members at the time — above that the exit rule would switch the
mode straight back off, so the proposal is refused rather than passed and
quietly undone.

## Managing Your Profile

Visit `/profile` to see your profile, including:

- Display name, bio, and avatar
- Trust score with a visual bar
- Your role and join date
- Your posts and vouches (received and given)

Click "Edit profile" to update your display name (required, max 100 characters), bio (max 500 characters), and avatar URL.

Account settings (email, password) are managed through Kratos at `/auth/settings`.
