package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

// PostRepository abstracts post persistence using domain types.
type PostRepository interface {
	CreatePost(ctx context.Context, post *domain.Post) error
	GetPostByID(ctx context.Context, id string) (*domain.Post, error)
	ListPosts(ctx context.Context, cursor string, limit int) ([]*domain.Post, error)
	ListPostsByAuthor(ctx context.Context, authorID string, limit int) ([]*domain.Post, error)
	UpdatePostBody(ctx context.Context, id string, body string) (*domain.Post, error)
	UpdatePostStatus(ctx context.Context, id string, status domain.PostStatus, reason, removedBy string) error
}

// FeedCacher is an optional cache layer for the post feed.
type FeedCacher interface {
	GetFeed(ctx context.Context, cursor string, limit int) ([]*domain.Post, error)
	InvalidateOnCreate(ctx context.Context, post *domain.Post)
	InvalidateOnUpdate(ctx context.Context, post *domain.Post)
	InvalidateOnDelete(ctx context.Context, postID string)
}

// PostService orchestrates post business logic.
type PostService struct {
	repo      PostRepository
	feedCache FeedCacher
	now       func() time.Time
}

func NewPostService(repo PostRepository, clock func() time.Time) *PostService {
	if clock == nil {
		clock = time.Now
	}
	return &PostService{
		repo: repo,
		now:  clock,
	}
}

// SetFeedCache attaches an optional feed cache to the service.
func (s *PostService) SetFeedCache(fc FeedCacher) {
	s.feedCache = fc
}

// Create publishes a post by author.
//
// It takes the whole user rather than just the denormalized author fields
// because it also authorizes the write. The handler checks CanPost too, as the
// early and friendlier rejection, but this is the check that cannot be
// bypassed: before it existed, one line in one handler was the only thing
// between a muted user and a post.
//
// The author fields are denormalized onto the post here rather than filled in
// afterwards because the post is written to the feed cache before Create
// returns: a post completed after the fact would be cached, and served, with no
// author name.
func (s *PostService) Create(ctx context.Context, author *domain.User, body, imagePath string) (*domain.Post, error) {
	if author == nil {
		return nil, fmt.Errorf("%w: no author", ErrForbidden)
	}
	if !author.CanPost(s.now()) {
		return nil, fmt.Errorf("%w: posting not allowed", ErrForbidden)
	}
	if err := validateBody(body); err != nil {
		return nil, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generating post id: %w", err)
	}

	post := &domain.Post{
		ID:                id.String(),
		AuthorID:          author.ID,
		AuthorDisplayName: author.DisplayName,
		AuthorAvatarURL:   author.AvatarURL,
		Body:              body,
		ImagePath:         imagePath,
		Status:            domain.PostVisible,
		CreatedAt:         s.now(),
	}

	if err := s.repo.CreatePost(ctx, post); err != nil {
		return nil, fmt.Errorf("creating post: %w", err)
	}

	if s.feedCache != nil {
		s.feedCache.InvalidateOnCreate(ctx, post)
	}

	return post, nil
}

func (s *PostService) GetByID(ctx context.Context, id string) (*domain.Post, error) {
	return s.repo.GetPostByID(ctx, id)
}

func (s *PostService) ListFeed(ctx context.Context, cursor string, limit int) ([]*domain.Post, error) {
	if s.feedCache != nil {
		return s.feedCache.GetFeed(ctx, cursor, limit)
	}
	return s.repo.ListPosts(ctx, cursor, limit)
}

func (s *PostService) ListByAuthor(ctx context.Context, authorID string, limit int) ([]*domain.Post, error) {
	return s.repo.ListPostsByAuthor(ctx, authorID, limit)
}

func (s *PostService) UpdateBody(ctx context.Context, id, userID, body string) (*domain.Post, error) {
	if err := validateBody(body); err != nil {
		return nil, err
	}

	post, err := s.repo.GetPostByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if !post.CanEdit(userID, s.now()) {
		return nil, ErrEditWindow
	}

	updated, err := s.repo.UpdatePostBody(ctx, id, body)
	if err != nil {
		return nil, err
	}

	// The cache stores whole posts, so an edit leaves the old body in the feed
	// until the entry is evicted by length — not bounded by the feed TTL.
	if s.feedCache != nil {
		s.feedCache.InvalidateOnUpdate(ctx, updated)
	}

	return updated, nil
}

func (s *PostService) Delete(ctx context.Context, id, userID string) error {
	post, err := s.repo.GetPostByID(ctx, id)
	if err != nil {
		return err
	}

	if post.AuthorID != userID {
		return ErrForbidden
	}

	// No moderator and no reason: the author took their own post down.
	if err := s.repo.UpdatePostStatus(ctx, id, domain.PostRemovedByAuthor, "", ""); err != nil {
		return err
	}

	if s.feedCache != nil {
		s.feedCache.InvalidateOnDelete(ctx, id)
	}

	return nil
}

// RemoveByModerator takes a post down on a moderator's authority, recording
// why. It is the only path that ever writes PostRemovedByMod.
//
// The moderator is passed in whole and authorized here rather than trusted from
// the route guard, for the reason spelled out on Create: a single line in
// routes.go is otherwise all that stands between an ordinary member and taking
// down anyone's post. The handler's guard is the early rejection; this is the
// one that cannot be bypassed.
//
// The reason is stored on the post as a moderator's private note. It is not
// serialized by domain.Post — see the comment on RemovalReason — so exposing it
// to a moderator UI takes an explicit response type, deliberately.
func (s *PostService) RemoveByModerator(ctx context.Context, moderator *domain.User, postID, reason string) error {
	if moderator == nil || !moderator.CanModerate() {
		return fmt.Errorf("%w: moderator role required", ErrForbidden)
	}

	reason, err := validateRemovalReason(reason)
	if err != nil {
		return err
	}

	post, err := s.repo.GetPostByID(ctx, postID)
	if err != nil {
		return err
	}

	if !canRemoveByModerator(post.Status) {
		return fmt.Errorf("%w: post is already removed", ErrValidation)
	}

	// The whole audit trail for a removal is these two columns on the post:
	// removal_reason and removed_by. Deliberately NO moderation_actions row.
	//
	// This is not an oversight, and a future contributor should not "fix" it.
	// Every row in moderation_actions flows through PropagatePenalties, which
	// indexes domain.DirectPenalty by the action's severity and then walks the
	// vouch graph — so giving removal an action type would automatically dock
	// trust from the author AND from everyone who ever vouched for them, every
	// time a post came down. That table means "punishment applied to a person".
	//
	// Removing a post is an action against a piece of content. A moderator who
	// judges the person to be the problem still has warn, mute, suspend and ban,
	// and has to choose one deliberately.
	if err := s.repo.UpdatePostStatus(ctx, postID, domain.PostRemovedByMod, reason, moderator.ID); err != nil {
		return err
	}

	// Same invalidation the author-deletion path performs, and for the same
	// reason: the cache stores whole posts, so a removed post keeps being served
	// to the whole town until its entry is evicted by length.
	if s.feedCache != nil {
		s.feedCache.InvalidateOnDelete(ctx, postID)
	}

	return nil
}

// maxRemovalReasonLen bounds a moderator's removal note, matching the limits on
// the other two free-text moderation fields (maxReportReasonLen,
// maxActionReasonLen). The posts column is TEXT, so this is the only bound.
const maxRemovalReasonLen = 1000

// validateRemovalReason normalizes and checks a moderator's removal note, and
// returns the trimmed value to persist.
//
// Unlike an author's own deletion, which stores "", the reason is mandatory: it
// is the only record of why a moderator overrode a member's speech, and a
// removal nobody can account for is the thing this feature exists to avoid. The
// bound is in bytes, matching validateReportReason.
func validateRemovalReason(reason string) (string, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "", fmt.Errorf("%w: reason must not be empty", ErrValidation)
	}
	if len(reason) > maxRemovalReasonLen {
		return "", fmt.Errorf("%w: reason exceeds %d characters", ErrValidation, maxRemovalReasonLen)
	}
	return reason, nil
}

// canRemoveByModerator reports whether a post in this state may be removed by a
// moderator.
//
// Only a visible post may be. Re-removing an already-removed one would rewrite
// the record for no gain: on a post the author deleted themselves it would
// replace removed_by_author, erasing the fact that the author acted first, and
// on a post another moderator removed it would overwrite their note with a
// second one. Neither post is visible to the town either way, so there is
// nothing to gain by allowing it. This mirrors SubmitReport, which likewise
// refuses to act on a post that is no longer visible.
func canRemoveByModerator(status domain.PostStatus) bool {
	return status == domain.PostVisible
}

func validateBody(body string) error {
	if strings.TrimSpace(body) == "" {
		return fmt.Errorf("%w: body must not be empty", ErrValidation)
	}
	if len(body) > domain.MaxPostBodyLength {
		return fmt.Errorf("%w: body exceeds %d characters", ErrValidation, domain.MaxPostBodyLength)
	}
	return nil
}
