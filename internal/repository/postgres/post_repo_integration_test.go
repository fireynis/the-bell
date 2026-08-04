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
	newPost(t, pool, "post-removed", author.ID, domain.PostVisible, time.Now())
	if err := repo.UpdatePostStatus(ctx, "post-removed", domain.PostRemovedByMod, "off topic"); err != nil {
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

	now := time.Now()
	newPost(t, pool, "author-visible", author.ID, domain.PostVisible, now)
	newPost(t, pool, "author-removed", author.ID, domain.PostRemovedByMod, now.Add(-1*time.Minute))
	newPost(t, pool, "author-deleted", author.ID, domain.PostRemovedByAuthor, now.Add(-2*time.Minute))
	newPost(t, pool, "other-visible", other.ID, domain.PostVisible, now)

	// The removal carries a note that must never leave the moderation tools.
	const privateNote = "harassment; second warning issued"
	if err := repo.UpdatePostStatus(ctx, "author-removed", domain.PostRemovedByMod, privateNote); err != nil {
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

func TestPostRepo_UpdatePostBody(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("post-author"), domain.RoleMember, 50)
	newPost(t, pool, "post-1", author.ID, domain.PostVisible, time.Now())

	updated, err := repo.UpdatePostBody(ctx, "post-1", "edited body")
	if err != nil {
		t.Fatalf("UpdatePostBody: %v", err)
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
func TestPostRepo_UpdatePostBody_ReturnsAuthorFields(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := authorNamed(t, pool, "post-author", "Ada")
	newPost(t, pool, "post-1", author.ID, domain.PostVisible, time.Now())

	updated, err := repo.UpdatePostBody(ctx, "post-1", "edited body")
	if err != nil {
		t.Fatalf("UpdatePostBody: %v", err)
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
func TestPostRepo_UpdatePostBody_JoinDoesNotHideMissingPosts(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	repo := postgres.NewPostRepo(postgres.New(pool))

	author := authorNamed(t, pool, "post-author", "Ada")
	newPost(t, pool, "post-1", author.ID, domain.PostVisible, time.Now())

	got, err := repo.UpdatePostBody(ctx, "no-such-post", "body")
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

func TestPostRepo_UpdatePostBody_NotFound(t *testing.T) {
	pool := testsupport.TestDB(t)
	repo := postgres.NewPostRepo(postgres.New(pool))

	got, err := repo.UpdatePostBody(context.Background(), "no-such-post", "body")
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

	if _, err := repo.UpdatePostBody(ctx, "post-edited", "edited body"); err != nil {
		t.Fatalf("UpdatePostBody: %v", err)
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
	newPost(t, pool, "post-1", author.ID, domain.PostVisible, time.Now())
	newPost(t, pool, "post-2", author.ID, domain.PostVisible, time.Now())

	if err := repo.UpdatePostStatus(ctx, "post-1", domain.PostRemovedByMod, "off topic"); err != nil {
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
		UpdatePostStatus(context.Background(), "no-such-post", domain.PostRemovedByMod, "reason")
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
