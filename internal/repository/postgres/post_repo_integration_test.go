//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/service"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The read paths differ in which SQL they use — GetPostByID, the two feed
// queries and ListPostsByAuthor are four separate statements — so the filters
// and joins they do or do not share are only observable against a database.

// authorNamed creates a user and fills in the profile. TestUser leaves the
// display name empty — FindOrCreate does not know it yet at sign-up — so a test
// that wants to see the users join produce something has to set it.
func authorNamed(t *testing.T, pool *pgxpool.Pool, suffix, displayName string) *domain.User {
	t.Helper()

	user := testsupport.TestUser(t, pool, testsupport.UniqueKratosID(suffix), domain.RoleMember, 50)
	updated, err := postgres.NewUserRepo(postgres.New(pool)).
		UpdateUserProfile(context.Background(), user.ID, displayName, "", "/img/"+suffix+".jpg")
	if err != nil {
		t.Fatalf("UpdateUserProfile: %v", err)
	}
	return updated
}

func TestPostRepo_GetPostByID(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := authorNamed(t, pool, "post-author", "Ada")
	created := time.Now().Truncate(time.Millisecond)
	newPost(t, pool, "post-1", author.ID, domain.PostVisible, created)

	got, err := repo.GetPostByID(ctx, "post-1")
	if err != nil {
		t.Fatalf("GetPostByID: %v", err)
	}
	if got.ID != "post-1" || got.AuthorID != author.ID {
		t.Errorf("got %+v, want post-1 by %s", got, author.ID)
	}
	// GetPostByID joins users, so the feed card can render without a second query.
	if got.AuthorDisplayName != "Ada" || got.AuthorAvatarURL != author.AvatarURL {
		t.Errorf("author = %q/%q, want Ada/%s", got.AuthorDisplayName, got.AuthorAvatarURL, author.AvatarURL)
	}
	if got.EditedAt != nil {
		t.Errorf("edited_at = %v on a post that was never edited, want nil", *got.EditedAt)
	}
	if diff := got.CreatedAt.Sub(created).Abs(); diff > time.Millisecond {
		t.Errorf("created_at = %v, want ~%v", got.CreatedAt, created)
	}
}

func TestPostRepo_GetPostByID_NotFound(t *testing.T) {
	pool := testsupport.TestDB(t)
	repo := postgres.NewPostRepo(postgres.New(pool))

	got, err := repo.GetPostByID(context.Background(), "no-such-post")
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("err = %v, want service.ErrNotFound", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

// A removed post is still readable by id — that is what lets the author and a
// moderator see the removal reason — so the 'visible' filter is the feed's job,
// not GetPostByID's.
func TestPostRepo_GetPostByID_ReturnsRemovedPosts(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("post-author"), domain.RoleMember, 50)
	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("post-mod"), domain.RoleModerator, 90)
	newPost(t, pool, "post-removed", author.ID, domain.PostVisible, time.Now())
	if err := repo.UpdatePostStatus(ctx, "post-removed", domain.PostRemovedByMod, "off topic", moderator.ID); err != nil {
		t.Fatalf("UpdatePostStatus: %v", err)
	}

	got, err := repo.GetPostByID(ctx, "post-removed")
	if err != nil {
		t.Fatalf("GetPostByID: %v", err)
	}
	if got.Status != domain.PostRemovedByMod {
		t.Errorf("status = %q, want %q", got.Status, domain.PostRemovedByMod)
	}
	if got.RemovalReason != "off topic" {
		t.Errorf("removal_reason = %q, want %q", got.RemovalReason, "off topic")
	}
}

// The feed pages by descending id and shows only visible posts.
func TestPostRepo_ListPosts_FeedFiltersAndPages(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := authorNamed(t, pool, "post-author", "Ada")
	now := time.Now()
	for _, id := range []string{"post-a", "post-b", "post-c", "post-d"} {
		newPost(t, pool, id, author.ID, domain.PostVisible, now)
	}
	newPost(t, pool, "post-e-removed", author.ID, domain.PostRemovedByMod, now)
	newPost(t, pool, "post-f-deleted", author.ID, domain.PostRemovedByAuthor, now)

	first, err := repo.ListPosts(ctx, "", 3)
	if err != nil {
		t.Fatalf("ListPosts: %v", err)
	}
	if len(first) != 3 {
		t.Fatalf("got %d posts, want 3", len(first))
	}
	if first[0].ID != "post-d" || first[1].ID != "post-c" || first[2].ID != "post-b" {
		t.Errorf("first page = %s, want post-d, post-c, post-b in descending id order", postIDs(first))
	}
	if first[0].AuthorDisplayName != "Ada" {
		t.Errorf("feed row author = %q, want %q from the users join", first[0].AuthorDisplayName, "Ada")
	}

	// Paging from the last id of the first page continues without a gap or a
	// repeat.
	second, err := repo.ListPosts(ctx, first[len(first)-1].ID, 3)
	if err != nil {
		t.Fatalf("ListPosts (cursor): %v", err)
	}
	if len(second) != 1 || second[0].ID != "post-a" {
		t.Errorf("second page = %s, want just post-a", postIDs(second))
	}

	for _, p := range append(first, second...) {
		if p.Status != domain.PostVisible {
			t.Errorf("post %s with status %q reached the feed", p.ID, p.Status)
		}
	}
}

func TestPostRepo_ListPosts_Empty(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	for _, cursor := range []string{"", "post-zzz"} {
		got, err := repo.ListPosts(ctx, cursor, 10)
		if err != nil {
			t.Fatalf("ListPosts(%q): %v", cursor, err)
		}
		if len(got) != 0 {
			t.Errorf("ListPosts(%q) = %s, want none", cursor, postIDs(got))
		}
	}
}

// A profile is a public listing, so it shows what the feed shows: only visible
// posts. A moderator-removed post carries removal_reason — a moderator's
// private note — and this listing used to hand it to anyone who could view the
// profile.
func TestPostRepo_ListPostsByAuthor_ExcludesRemovedPosts(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("post-author"), domain.RoleMember, 50)
	other := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("post-other"), domain.RoleMember, 50)
	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("post-mod"), domain.RoleModerator, 90)

	now := time.Now()
	newPost(t, pool, "author-visible", author.ID, domain.PostVisible, now)
	newPost(t, pool, "author-removed", author.ID, domain.PostRemovedByMod, now.Add(-1*time.Minute))
	newPost(t, pool, "author-deleted", author.ID, domain.PostRemovedByAuthor, now.Add(-2*time.Minute))
	newPost(t, pool, "other-visible", other.ID, domain.PostVisible, now)

	// The removal carries a note that must never leave the moderation tools.
	const privateNote = "harassment; second warning issued"
	if err := repo.UpdatePostStatus(ctx, "author-removed", domain.PostRemovedByMod, privateNote, moderator.ID); err != nil {
		t.Fatalf("UpdatePostStatus: %v", err)
	}

	got, err := repo.ListPostsByAuthor(ctx, author.ID, 10)
	if err != nil {
		t.Fatalf("ListPostsByAuthor: %v", err)
	}

	if len(got) != 1 || got[0].ID != "author-visible" {
		t.Fatalf("got %s, want only author-visible", postIDs(got))
	}
	for _, p := range got {
		if p.AuthorID != author.ID {
			t.Errorf("post %s by %s leaked into the author listing", p.ID, p.AuthorID)
		}
		if p.Status != domain.PostVisible {
			t.Errorf("post %s with status %q reached the profile listing", p.ID, p.Status)
		}
		if p.RemovalReason != "" {
			t.Errorf("post %s carries removal_reason %q", p.ID, p.RemovalReason)
		}
	}
}

func TestPostRepo_ListPostsByAuthor_OrdersAndLimits(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := authorNamed(t, pool, "post-author", "Ada")
	now := time.Now()
	newPost(t, pool, "oldest", author.ID, domain.PostVisible, now.Add(-3*time.Hour))
	newPost(t, pool, "middle", author.ID, domain.PostVisible, now.Add(-2*time.Hour))
	newPost(t, pool, "newest", author.ID, domain.PostVisible, now.Add(-1*time.Hour))

	got, err := repo.ListPostsByAuthor(ctx, author.ID, 2)
	if err != nil {
		t.Fatalf("ListPostsByAuthor: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d posts, want the limit of 2", len(got))
	}
	if got[0].ID != "newest" || got[1].ID != "middle" {
		t.Errorf("order = %s, want newest first", postIDs(got))
	}
	if got[0].AuthorDisplayName != "Ada" {
		t.Errorf("author listing display name = %q, want %q from the users join", got[0].AuthorDisplayName, "Ada")
	}

	empty, err := repo.ListPostsByAuthor(ctx, "no-such-author", 10)
	if err != nil {
		t.Fatalf("ListPostsByAuthor: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("got %s for an unknown author, want none", postIDs(empty))
	}
}

func TestPostRepo_UpdatePostContent(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("post-author"), domain.RoleMember, 50)
	newPost(t, pool, "post-1", author.ID, domain.PostVisible, time.Now())

	updated, err := repo.UpdatePostContent(ctx, "post-1", "edited body", "")
	if err != nil {
		t.Fatalf("UpdatePostContent: %v", err)
	}
	if updated.Body != "edited body" {
		t.Errorf("body = %q, want %q", updated.Body, "edited body")
	}
	// edited_at is set by the UPDATE, and it is what the UI renders as "edited".
	if updated.EditedAt == nil {
		t.Error("edited_at is nil after an edit")
	}

	// The change is durable, and the read path agrees.
	reread, err := repo.GetPostByID(ctx, "post-1")
	if err != nil {
		t.Fatalf("GetPostByID: %v", err)
	}
	if reread.Body != "edited body" || reread.EditedAt == nil {
		t.Errorf("re-read post = %+v, want the edited body and a non-nil edited_at", reread)
	}
}

// The edit response is handed straight to the client, so it must carry the same
// author fields as every read path. It previously did not — the UPDATE had no
// users join — and editing a post made the author's name vanish from the card.
func TestPostRepo_UpdatePostContent_ReturnsAuthorFields(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := authorNamed(t, pool, "post-author", "Ada")
	newPost(t, pool, "post-1", author.ID, domain.PostVisible, time.Now())

	updated, err := repo.UpdatePostContent(ctx, "post-1", "edited body", "")
	if err != nil {
		t.Fatalf("UpdatePostContent: %v", err)
	}
	if updated.AuthorDisplayName != "Ada" || updated.AuthorAvatarURL != author.AvatarURL {
		t.Errorf("author fields = %q/%q, want Ada/%s", updated.AuthorDisplayName, updated.AuthorAvatarURL, author.AvatarURL)
	}

	// The edit response and a fresh read of the same post agree field for field;
	// only the body and edited_at differ from what was stored before.
	reread, err := repo.GetPostByID(ctx, "post-1")
	if err != nil {
		t.Fatalf("GetPostByID: %v", err)
	}
	if updated.AuthorDisplayName != reread.AuthorDisplayName || updated.AuthorAvatarURL != reread.AuthorAvatarURL {
		t.Errorf("edit response author = %q/%q but read path says %q/%q",
			updated.AuthorDisplayName, updated.AuthorAvatarURL, reread.AuthorDisplayName, reread.AuthorAvatarURL)
	}
	if updated.ID != reread.ID || updated.AuthorID != reread.AuthorID ||
		updated.Body != reread.Body || updated.Status != reread.Status ||
		updated.ImagePath != reread.ImagePath || updated.RemovalReason != reread.RemovalReason {
		t.Errorf("edit response %+v disagrees with the read path %+v", updated, reread)
	}
	if updated.EditedAt == nil || reread.EditedAt == nil {
		t.Error("edited_at is nil on one of the two paths")
	}
}

// The UPDATE now joins users, so a post whose author row was missing would
// update nothing. Every post has an author FK, making that unreachable — but an
// unknown post id must still come back as ErrNotFound rather than as a silently
// empty result.
func TestPostRepo_UpdatePostContent_JoinDoesNotHideMissingPosts(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := authorNamed(t, pool, "post-author", "Ada")
	newPost(t, pool, "post-1", author.ID, domain.PostVisible, time.Now())

	got, err := repo.UpdatePostContent(ctx, "no-such-post", "body", "")
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("err = %v, want service.ErrNotFound", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}

	// And the real post was not touched by the failed update.
	untouched, err := repo.GetPostByID(ctx, "post-1")
	if err != nil {
		t.Fatalf("GetPostByID: %v", err)
	}
	if untouched.EditedAt != nil {
		t.Error("an unrelated post was marked as edited")
	}
}

// An image description has to survive every way a post is read back, for the
// same reason edited_at does: the four read statements map their rows
// separately, so any one of them can drop a column on its own. A description
// that reaches the single-post view but not the feed is worse than none — the
// image is announced properly on one page and silently on the next.
func TestPostRepo_AltTextReachesEveryReadPath(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	const alt = "A heron on the frozen millpond"
	author := authorNamed(t, pool, "post-author", "Ada")
	now := time.Now()

	described := &domain.Post{
		ID:        "post-described",
		AuthorID:  author.ID,
		Body:      "look at this",
		ImagePath: "/uploads/heron.jpg",
		AltText:   alt,
		Status:    domain.PostVisible,
		CreatedAt: now,
	}
	if err := repo.CreatePost(ctx, described); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}
	// A second, newer post so the cursored feed page has something to page past.
	newPost(t, pool, "post-later", author.ID, domain.PostVisible, now.Add(time.Minute))

	single, err := repo.GetPostByID(ctx, "post-described")
	if err != nil {
		t.Fatalf("GetPostByID: %v", err)
	}
	if single.AltText != alt {
		t.Errorf("GetPostByID alt_text = %q, want %q", single.AltText, alt)
	}

	firstPage, err := repo.ListPosts(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListPosts: %v", err)
	}
	cursored, err := repo.ListPosts(ctx, "post-later", 10)
	if err != nil {
		t.Fatalf("ListPosts (cursored): %v", err)
	}
	byAuthor, err := repo.ListPostsByAuthor(ctx, author.ID, 10)
	if err != nil {
		t.Fatalf("ListPostsByAuthor: %v", err)
	}

	for name, posts := range map[string][]*domain.Post{
		"feed first page": firstPage,
		"feed next page":  cursored,
		"author listing":  byAuthor,
	} {
		found := false
		for _, p := range posts {
			if p.ID != "post-described" {
				continue
			}
			found = true
			if p.AltText != alt {
				t.Errorf("%s: alt_text = %q, want %q", name, p.AltText, alt)
			}
		}
		if !found {
			t.Errorf("%s: post-described missing from %s", name, postIDs(posts))
		}
	}

	// And the edit response, which is the fifth mapping of the same row.
	edited, err := repo.UpdatePostContent(ctx, "post-described", "look at this again", alt)
	if err != nil {
		t.Fatalf("UpdatePostContent: %v", err)
	}
	if edited.AltText != alt {
		t.Errorf("edit response alt_text = %q, want %q", edited.AltText, alt)
	}
}

// The column is written by the edit, not merely echoed back from the argument:
// a RETURNING clause that forgot alt_text would still hand the caller the value
// it was given, so the check that matters is a fresh read.
func TestPostRepo_UpdatePostContent_PersistsAltText(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := authorNamed(t, pool, "post-author", "Ada")
	if err := repo.CreatePost(ctx, &domain.Post{
		ID:        "post-1",
		AuthorID:  author.ID,
		Body:      "look at this",
		ImagePath: "/uploads/heron.jpg",
		AltText:   "A bird",
		Status:    domain.PostVisible,
		CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreatePost: %v", err)
	}

	if _, err := repo.UpdatePostContent(ctx, "post-1", "look at this", "A heron on the frozen millpond"); err != nil {
		t.Fatalf("UpdatePostContent: %v", err)
	}

	reread, err := repo.GetPostByID(ctx, "post-1")
	if err != nil {
		t.Fatalf("GetPostByID: %v", err)
	}
	if reread.AltText != "A heron on the frozen millpond" {
		t.Errorf("stored alt_text = %q, want the edited description", reread.AltText)
	}

	// And clearing it writes the empty string rather than leaving the old one.
	if _, err := repo.UpdatePostContent(ctx, "post-1", "look at this", ""); err != nil {
		t.Fatalf("UpdatePostContent (clearing): %v", err)
	}
	cleared, err := repo.GetPostByID(ctx, "post-1")
	if err != nil {
		t.Fatalf("GetPostByID: %v", err)
	}
	if cleared.AltText != "" {
		t.Errorf("stored alt_text = %q after clearing, want empty", cleared.AltText)
	}
}

func TestPostRepo_UpdatePostContent_NotFound(t *testing.T) {
	pool := testsupport.TestDB(t)
	repo := postgres.NewPostRepo(postgres.New(pool))

	got, err := repo.UpdatePostContent(context.Background(), "no-such-post", "body", "")
	if !errors.Is(err, service.ErrNotFound) {
		t.Errorf("err = %v, want service.ErrNotFound", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

// edited_at is what the UI renders as the "edited" marker, and it has to
// survive into every read path — the two feed queries and the author listing
// each map the row separately, so each could drop it independently.
func TestPostRepo_EditedAtReachesEveryReadPath(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := authorNamed(t, pool, "post-author", "Ada")
	now := time.Now()
	newPost(t, pool, "post-edited", author.ID, domain.PostVisible, now)
	newPost(t, pool, "post-untouched", author.ID, domain.PostVisible, now)

	if _, err := repo.UpdatePostContent(ctx, "post-edited", "edited body", ""); err != nil {
		t.Fatalf("UpdatePostContent: %v", err)
	}

	// First feed page (no cursor), a cursored feed page, and the author listing.
	firstPage, err := repo.ListPosts(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListPosts: %v", err)
	}
	cursored, err := repo.ListPosts(ctx, "post-untouched", 10)
	if err != nil {
		t.Fatalf("ListPosts (cursor): %v", err)
	}
	byAuthor, err := repo.ListPostsByAuthor(ctx, author.ID, 10)
	if err != nil {
		t.Fatalf("ListPostsByAuthor: %v", err)
	}

	for name, posts := range map[string][]*domain.Post{
		"feed first page":  firstPage,
		"feed with cursor": cursored,
		"author listing":   byAuthor,
	} {
		var seen bool
		for _, p := range posts {
			switch p.ID {
			case "post-edited":
				seen = true
				if p.EditedAt == nil {
					t.Errorf("%s: edited post has a nil edited_at", name)
				}
			case "post-untouched":
				if p.EditedAt != nil {
					t.Errorf("%s: untouched post has edited_at = %v", name, *p.EditedAt)
				}
			}
		}
		if !seen {
			t.Errorf("%s: the edited post is missing entirely (%s)", name, postIDs(posts))
		}
	}
}

func TestPostRepo_UpdatePostStatus(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("post-author"), domain.RoleMember, 50)
	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("post-mod"), domain.RoleModerator, 90)
	newPost(t, pool, "post-1", author.ID, domain.PostVisible, time.Now())
	newPost(t, pool, "post-2", author.ID, domain.PostVisible, time.Now())

	if err := repo.UpdatePostStatus(ctx, "post-1", domain.PostRemovedByMod, "off topic", moderator.ID); err != nil {
		t.Fatalf("UpdatePostStatus: %v", err)
	}

	got, err := repo.GetPostByID(ctx, "post-1")
	if err != nil {
		t.Fatalf("GetPostByID: %v", err)
	}
	if got.Status != domain.PostRemovedByMod || got.RemovalReason != "off topic" {
		t.Errorf("got %q/%q, want removed_by_mod/off topic", got.Status, got.RemovalReason)
	}

	// It leaves the feed, and the untouched post stays.
	feed, err := repo.ListPosts(ctx, "", 10)
	if err != nil {
		t.Fatalf("ListPosts: %v", err)
	}
	if len(feed) != 1 || feed[0].ID != "post-2" {
		t.Errorf("feed = %s, want only post-2", postIDs(feed))
	}
}

// Soft-deleting a post that does not exist is a no-op rather than an error: the
// moderation queue may act on a row someone else already handled.
func TestPostRepo_UpdatePostStatus_UnknownIDIsNoop(t *testing.T) {
	pool := testsupport.TestDB(t)

	err := postgres.NewPostRepo(postgres.New(pool)).
		UpdatePostStatus(context.Background(), "no-such-post", domain.PostRemovedByMod, "reason", "")
	if err != nil {
		t.Errorf("UpdatePostStatus on an unknown post: %v", err)
	}
}

func postIDs(posts []*domain.Post) string {
	ids := make([]string, len(posts))
	for i, p := range posts {
		ids[i] = p.ID
	}
	return strings.Join(ids, ", ")
}

// removed_by is what makes the removal attributable. It is a foreign key to
// users, so the author-deletion path — which has no moderator — must store NULL
// rather than an id nobody has, and must read back as empty.
func TestPostRepo_UpdatePostStatus_RecordsTheRemovingModerator(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("removedby-author"), domain.RoleMember, 50)
	moderator := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("removedby-mod"), domain.RoleModerator, 90)

	newPost(t, pool, "by-mod", author.ID, domain.PostVisible, time.Now())
	newPost(t, pool, "by-author", author.ID, domain.PostVisible, time.Now())

	if err := repo.UpdatePostStatus(ctx, "by-mod", domain.PostRemovedByMod, "off topic", moderator.ID); err != nil {
		t.Fatalf("UpdatePostStatus (moderator): %v", err)
	}
	if err := repo.UpdatePostStatus(ctx, "by-author", domain.PostRemovedByAuthor, "", ""); err != nil {
		t.Fatalf("UpdatePostStatus (author): %v", err)
	}

	byMod, err := repo.GetPostByID(ctx, "by-mod")
	if err != nil {
		t.Fatalf("GetPostByID(by-mod): %v", err)
	}
	if byMod.RemovedBy != moderator.ID {
		t.Errorf("RemovedBy = %q, want the moderator %q", byMod.RemovedBy, moderator.ID)
	}

	byAuthor, err := repo.GetPostByID(ctx, "by-author")
	if err != nil {
		t.Fatalf("GetPostByID(by-author): %v", err)
	}
	if byAuthor.RemovedBy != "" {
		t.Errorf("RemovedBy = %q on an author deletion, want empty", byAuthor.RemovedBy)
	}

	// NULL in the column, not the empty string: "" is not a valid users.id and
	// would have to be rejected by the foreign key if it were ever written.
	var stored *string
	if err := pool.QueryRow(ctx, `SELECT removed_by FROM posts WHERE id = $1`, "by-author").Scan(&stored); err != nil {
		t.Fatalf("reading removed_by: %v", err)
	}
	if stored != nil {
		t.Errorf("removed_by = %q on an author deletion, want NULL", *stored)
	}
}

// The column is a real foreign key, so a removal naming a user who does not
// exist is rejected by the database rather than silently recorded.
func TestPostRepo_UpdatePostStatus_UnknownModeratorIsRejected(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("fk-author"), domain.RoleMember, 50)
	newPost(t, pool, "fk-post", author.ID, domain.PostVisible, time.Now())

	err := repo.UpdatePostStatus(ctx, "fk-post", domain.PostRemovedByMod, "off topic", "no-such-user")
	if err == nil {
		t.Fatal("UpdatePostStatus accepted a moderator id that is not a user")
	}
}
