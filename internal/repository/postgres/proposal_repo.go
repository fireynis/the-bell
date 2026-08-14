package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// ProposalRepo adapts sqlc queries to service.ProposalStore.
type ProposalRepo struct {
	q *Queries
}

func NewProposalRepo(q *Queries) *ProposalRepo {
	return &ProposalRepo{q: q}
}

func (r *ProposalRepo) CreateProposal(ctx context.Context, p *domain.Proposal) error {
	_, err := r.q.CreateProposal(ctx, CreateProposalParams{
		ID:           p.ID,
		Type:         string(p.Type),
		TargetUserID: optionalText(p.TargetUserID),
		Rationale:    p.Rationale,
		CreatedBy:    p.CreatedBy,
		Status:       string(p.Status),
		CreatedAt:    pgtype.Timestamptz{Time: p.CreatedAt, Valid: true},
	})
	// The partial unique index in 00021 allows one open motion per
	// (type, target). The service looks first so the council gets a sentence
	// rather than a constraint name, but two councillors raising the same
	// motion at once both pass that look — and this is what stops the second.
	if isUniqueViolation(err) {
		return domain.ErrValidation
	}
	if isForeignKeyViolation(err) {
		return domain.ErrNotFound
	}
	return err
}

func (r *ProposalRepo) GetProposal(ctx context.Context, id string) (*domain.Proposal, error) {
	row, err := r.q.GetProposal(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return proposalFromRow(row), nil
}

func (r *ProposalRepo) ListOpenProposals(ctx context.Context) ([]domain.ProposalView, error) {
	rows, err := r.q.ListOpenProposals(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]domain.ProposalView, len(rows))
	for i, row := range rows {
		views[i] = domain.ProposalView{
			Proposal:             *proposalFromRow(row.Proposal),
			TargetDisplayName:    row.TargetDisplayName,
			CreatedByDisplayName: row.CreatedByDisplayName,
		}
	}
	return views, nil
}

func (r *ProposalRepo) ListDecidedProposals(ctx context.Context, limit int) ([]domain.ProposalView, error) {
	rows, err := r.q.ListDecidedProposals(ctx, int32Bound(limit))
	if err != nil {
		return nil, err
	}
	views := make([]domain.ProposalView, len(rows))
	for i, row := range rows {
		views[i] = domain.ProposalView{
			Proposal:             *proposalFromRow(row.Proposal),
			TargetDisplayName:    row.TargetDisplayName,
			CreatedByDisplayName: row.CreatedByDisplayName,
		}
	}
	return views, nil
}

func (r *ProposalRepo) FindOpenProposalByTypeAndTarget(ctx context.Context, t domain.ProposalType, targetID string) (*domain.Proposal, error) {
	row, err := r.q.FindOpenProposalByTypeAndTarget(ctx, FindOpenProposalByTypeAndTargetParams{
		Type:         string(t),
		TargetUserID: optionalText(targetID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return proposalFromRow(row), nil
}

// DecideProposal settles a motion. The query only matches a motion still open,
// so no rows means somebody else decided it first — reported as ErrNotFound,
// which is what the service reads as "re-read, theirs stands".
func (r *ProposalRepo) DecideProposal(ctx context.Context, id string, status domain.ProposalStatus, decidedAt time.Time) error {
	_, err := r.q.DecideProposal(ctx, DecideProposalParams{
		ID:        id,
		Status:    string(status),
		DecidedAt: pgtype.Timestamptz{Time: decidedAt, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

// optionalText maps the domain's empty string onto SQL NULL.
//
// The two spellings of "no target" are deliberately different on each side: the
// domain uses "" because every reader asks one question, and the column uses
// NULL because a foreign key cannot point at the empty string. This is the one
// place that translates between them.
func optionalText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func proposalFromRow(row Proposal) *domain.Proposal {
	p := &domain.Proposal{
		ID:        row.ID,
		Type:      domain.ProposalType(row.Type),
		Rationale: row.Rationale,
		CreatedBy: row.CreatedBy,
		Status:    domain.ProposalStatus(row.Status),
		CreatedAt: row.CreatedAt.Time,
	}
	if row.TargetUserID.Valid {
		p.TargetUserID = row.TargetUserID.String
	}
	if row.DecidedAt.Valid {
		t := row.DecidedAt.Time
		p.DecidedAt = &t
	}
	return p
}
