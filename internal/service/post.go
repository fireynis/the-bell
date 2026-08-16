package service

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/google/uuid"
)

// PostRepository abstracts post persistence using domain types.
type PostRepository interface {
	CreatePost(ctx context.Context, post *domain.Post) error
	GetPostByID(ctx context.Context, id string) (*domain.Post, error)
	ListPosts(ctx context.Context, cursor string, limit int) ([]*domain.Post, error)
	ListPostsByAuthor(ctx context.Context, authorID string, limit int) ([]*domain.Post, error)
	UpdatePostContent(ctx context.Context, id, body, altText string) (*domain.Post, error)
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
func (s *PostService) Create(ctx context.Context, author *domain.User, body, imagePath, altText string) (*domain.Post, error) {
	if author == nil {
		return nil, fmt.Errorf("%w: no author", ErrForbidden)
	}
	if !author.CanPost(s.now()) {
		return nil, fmt.Errorf("%w: posting not allowed", ErrForbidden)
	}
	if err := validateBody(body); err != nil {
		return nil, err
	}

	altText, err := validateAltText(altText, imagePath != "")
	if err != nil {
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
		AltText:           altText,
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

// UpdateContent edits an author's own post inside the edit window.
//
// altText is a pointer so that "leave the description alone" and "clear the
// description" are different requests. A nil pointer keeps whatever the post
// already carries; a pointer to "" clears it. Taking a plain string would mean
// every client that edits a typo has to remember to send the alt text back, and
// the one that forgets silently strips a blind neighbour's description off the
// image — a data loss with no error and no way to notice it from the UI.
//
// The description is validated against the image the post already has: alt text
// arrives on an edit long after the upload, and there is no path that adds or
// removes an image afterwards, so the stored image_path is the whole question.
func (s *PostService) UpdateContent(ctx context.Context, id, userID, body string, altText *string) (*domain.Post, error) {
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

	// Resolved before the write so the UPDATE always names a value: the column
	// is NOT NULL, and an omitted field means "unchanged", not "empty".
	newAltText := post.AltText
	if altText != nil {
		newAltText, err = validateAltText(*altText, post.ImagePath != "")
		if err != nil {
			return nil, err
		}
	}

	updated, err := s.repo.UpdatePostContent(ctx, id, body, newAltText)
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

// validateAltText normalizes and checks an image description, returning the
// trimmed value to persist.
//
// Empty is always fine, image or not: an author who does not describe their
// image still gets to post, and a client that sends the field on every edit —
// including for a post that has no image — is clearing nothing rather than
// making a mistake. What is refused is a non-empty description on a post with no
// image, because there is nothing for it to describe. Storing it would leave a
// string on the record that no reader ever hears and that nothing keeps true,
// and answering 400 tells the client its form is attached to the wrong post
// instead of silently accepting a write with no effect.
//
// Bounded in runes, matching domain.MaxAltTextRunes — see the constant for why
// this one field counts characters where the post body counts bytes.
func validateAltText(altText string, hasImage bool) (string, error) {
	altText = strings.TrimSpace(altText)
	if altText == "" {
		return "", nil
	}
	if !hasImage {
		return "", fmt.Errorf("%w: alt text requires an image", ErrValidation)
	}
	if utf8.RuneCountInString(altText) > domain.MaxAltTextRunes {
		return "", fmt.Errorf("%w: alt text exceeds %d characters", ErrValidation, domain.MaxAltTextRunes)
	}
	return altText, nil
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
