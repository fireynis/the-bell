package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/handler"
	"github.com/fireynis/the-bell/internal/middleware"
	"github.com/fireynis/the-bell/internal/service"
)

// proposalBody is the published contract, decoded. It is spelled out rather
// than reusing the handler's own type so that a renamed JSON key fails here —
// the frontend was built against these names.
type proposalBody struct {
	ID                   string  `json:"id"`
	Type                 string  `json:"type"`
	TargetUserID         string  `json:"target_user_id"`
	TargetDisplayName    string  `json:"target_display_name"`
	Rationale            string  `json:"rationale"`
	CreatedBy            string  `json:"created_by"`
	CreatedByDisplayName string  `json:"created_by_display_name"`
	Status               string  `json:"status"`
	CreatedAt            string  `json:"created_at"`
	DecidedAt            string  `json:"decided_at"`
	ApproveCount         int64   `json:"approve_count"`
	RejectCount          int64   `json:"reject_count"`
	CouncilSize          int64   `json:"council_size"`
	MyVote               *string `json:"my_vote"`
}

type listProposalsBody struct {
	Proposals []proposalBody `json:"proposals"`
}

type stubProposalService struct {
	view  *domain.ProposalView
	views []domain.ProposalView
	err   error

	gotType      domain.ProposalType
	gotTargetID  string
	gotRationale string
	gotVoteID    string
	gotApprove   bool
	gotDecided   bool
	gotLimit     int
	gotActor     *domain.User
}

func (s *stubProposalService) Create(_ context.Context, creator *domain.User, t domain.ProposalType, targetID, rationale string) (*domain.ProposalView, error) {
	s.gotActor, s.gotType, s.gotTargetID, s.gotRationale = creator, t, targetID, rationale
	if s.err != nil {
		return nil, s.err
	}
	return s.view, nil
}

func (s *stubProposalService) Vote(_ context.Context, voter *domain.User, proposalID string, approve bool) (*domain.ProposalView, error) {
	s.gotActor, s.gotVoteID, s.gotApprove = voter, proposalID, approve
	if s.err != nil {
		return nil, s.err
	}
	return s.view, nil
}

func (s *stubProposalService) List(_ context.Context, viewer *domain.User, decided bool, limit int) ([]domain.ProposalView, error) {
	s.gotActor, s.gotDecided, s.gotLimit = viewer, decided, limit
	if s.err != nil {
		return nil, s.err
	}
	return s.views, nil
}

var councilCaller = &domain.User{ID: "council-1", DisplayName: "Ada", Role: domain.RoleCouncil, IsActive: true}

// withCouncil puts a council member in the request context, which the auth
// middleware does in production.
func withCouncil(req *http.Request) *http.Request {
	return req.WithContext(middleware.WithUser(req.Context(), councilCaller))
}

func decidedAt(t time.Time) *time.Time { return &t }

func sampleView() *domain.ProposalView {
	choice := domain.VoteApprove
	return &domain.ProposalView{
		Proposal: domain.Proposal{
			ID:           "prop-1",
			Type:         domain.ProposalCouncilPromotion,
			TargetUserID: "mod-1",
			Rationale:    "she has run the queue for a year",
			CreatedBy:    "council-1",
			Status:       domain.ProposalOpen,
			CreatedAt:    time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC),
		},
		TargetDisplayName:    "Grace",
		CreatedByDisplayName: "Ada",
		ApproveCount:         2,
		RejectCount:          1,
		CouncilSize:          5,
		MyVote:               &choice,
	}
}

// --- List ---

func TestProposalHandler_List_ResponseShape(t *testing.T) {
	svc := &stubProposalService{views: []domain.ProposalView{*sampleView()}}
	h := handler.NewProposalHandler(svc)

	rec := httptest.NewRecorder()
	h.List(rec, withCouncil(httptest.NewRequest(http.MethodGet, "/api/v1/admin/proposals", nil)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	var body listProposalsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v; body: %s", err, rec.Body.String())
	}
	if len(body.Proposals) != 1 {
		t.Fatalf("%d proposals, want 1", len(body.Proposals))
	}

	got := body.Proposals[0]
	if got.ID != "prop-1" || got.Type != "council_promotion" || got.Status != "open" {
		t.Errorf("id/type/status = %q/%q/%q, want prop-1/council_promotion/open", got.ID, got.Type, got.Status)
	}
	if got.TargetUserID != "mod-1" || got.TargetDisplayName != "Grace" {
		t.Errorf("target = %q/%q, want mod-1/Grace", got.TargetUserID, got.TargetDisplayName)
	}
	if got.CreatedBy != "council-1" || got.CreatedByDisplayName != "Ada" {
		t.Errorf("proposer = %q/%q, want council-1/Ada", got.CreatedBy, got.CreatedByDisplayName)
	}
	if got.Rationale != "she has run the queue for a year" {
		t.Errorf("rationale = %q", got.Rationale)
	}
	if got.ApproveCount != 2 || got.RejectCount != 1 || got.CouncilSize != 5 {
		t.Errorf("tally = %d/%d of %d, want 2/1 of 5", got.ApproveCount, got.RejectCount, got.CouncilSize)
	}
	if got.MyVote == nil || *got.MyVote != "approve" {
		t.Errorf("my_vote = %v, want approve", got.MyVote)
	}
	if got.CreatedAt != "2026-08-14T09:00:00Z" {
		t.Errorf("created_at = %q, want RFC3339", got.CreatedAt)
	}
	// An open motion has no decision, so the key is absent rather than empty.
	if strings.Contains(rec.Body.String(), "decided_at") {
		t.Errorf("an open proposal published decided_at: %s", rec.Body.String())
	}
}

// my_vote must be present and null for a council member who has not voted. A
// missing key would be indistinguishable from a field the server forgot, and
// the client acts on the difference by enabling the buttons.
func TestProposalHandler_List_MyVoteIsNullWhenNotVoted(t *testing.T) {
	view := sampleView()
	view.MyVote = nil
	svc := &stubProposalService{views: []domain.ProposalView{*view}}
	h := handler.NewProposalHandler(svc)

	rec := httptest.NewRecorder()
	h.List(rec, withCouncil(httptest.NewRequest(http.MethodGet, "/api/v1/admin/proposals", nil)))

	if !strings.Contains(rec.Body.String(), `"my_vote":null`) {
		t.Errorf("body = %s, want an explicit null my_vote", rec.Body.String())
	}
}

// A motion about the town rather than a person omits the target keys entirely,
// so their absence is the answer to "is this about somebody".
func TestProposalHandler_List_TownWideProposalOmitsTheTarget(t *testing.T) {
	view := sampleView()
	view.Type = domain.ProposalBootstrapReentry
	view.TargetUserID = ""
	view.TargetDisplayName = ""
	svc := &stubProposalService{views: []domain.ProposalView{*view}}
	h := handler.NewProposalHandler(svc)

	rec := httptest.NewRecorder()
	h.List(rec, withCouncil(httptest.NewRequest(http.MethodGet, "/api/v1/admin/proposals", nil)))

	for _, absent := range []string{"target_user_id", "target_display_name"} {
		if strings.Contains(rec.Body.String(), absent) {
			t.Errorf("a town-wide proposal published %q: %s", absent, rec.Body.String())
		}
	}
}

func TestProposalHandler_List_DecidedProposalCarriesDecidedAt(t *testing.T) {
	view := sampleView()
	view.Status = domain.ProposalPassed
	view.DecidedAt = decidedAt(time.Date(2026, 8, 14, 10, 30, 0, 0, time.UTC))
	svc := &stubProposalService{views: []domain.ProposalView{*view}}
	h := handler.NewProposalHandler(svc)

	rec := httptest.NewRecorder()
	h.List(rec, withCouncil(httptest.NewRequest(http.MethodGet, "/api/v1/admin/proposals?status=decided", nil)))

	var body listProposalsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Proposals[0].Status != "passed" {
		t.Errorf("status = %q, want passed", body.Proposals[0].Status)
	}
	if body.Proposals[0].DecidedAt != "2026-08-14T10:30:00Z" {
		t.Errorf("decided_at = %q, want RFC3339", body.Proposals[0].DecidedAt)
	}
}

// ?status=decided is the only value that means anything; everything else,
// including a typo or an absent parameter, lists the open queue. Failing a
// misspelling with a 400 would hide live business behind a spelling mistake.
func TestProposalHandler_List_StatusParameter(t *testing.T) {
	tests := []struct {
		query       string
		wantDecided bool
	}{
		{"", false},
		{"?status=open", false},
		{"?status=decided", true},
		{"?status=DECIDED", false},
		{"?status=nonsense", false},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			svc := &stubProposalService{}
			h := handler.NewProposalHandler(svc)

			rec := httptest.NewRecorder()
			h.List(rec, withCouncil(httptest.NewRequest(http.MethodGet, "/api/v1/admin/proposals"+tt.query, nil)))

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			if svc.gotDecided != tt.wantDecided {
				t.Errorf("asked the service for decided=%v, want %v", svc.gotDecided, tt.wantDecided)
			}
		})
	}
}

// An empty queue is an empty array, never null. A client that maps over the
// field must not have to special-case a quiet week.
func TestProposalHandler_List_EmptyIsAnArray(t *testing.T) {
	h := handler.NewProposalHandler(&stubProposalService{})

	rec := httptest.NewRecorder()
	h.List(rec, withCouncil(httptest.NewRequest(http.MethodGet, "/api/v1/admin/proposals", nil)))

	if body := rec.Body.String(); !strings.Contains(body, `"proposals":[]`) {
		t.Errorf("body = %s, want an empty proposals array", body)
	}
}

func TestProposalHandler_List_UnauthenticatedIs401(t *testing.T) {
	h := handler.NewProposalHandler(&stubProposalService{})

	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/proposals", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

// --- Create ---

func TestProposalHandler_Create_PassesTheRequestThroughAndAnswers201(t *testing.T) {
	svc := &stubProposalService{view: sampleView()}
	h := handler.NewProposalHandler(svc)

	req := withCouncil(httptest.NewRequest(http.MethodPost, "/api/v1/admin/proposals",
		strings.NewReader(`{"type":"council_promotion","target_user_id":"mod-1","rationale":"she has run the queue for a year"}`)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body: %s", rec.Code, rec.Body.String())
	}
	if svc.gotType != domain.ProposalCouncilPromotion {
		t.Errorf("type = %q, want council_promotion", svc.gotType)
	}
	if svc.gotTargetID != "mod-1" {
		t.Errorf("target = %q, want mod-1", svc.gotTargetID)
	}
	if svc.gotRationale != "she has run the queue for a year" {
		t.Errorf("rationale = %q", svc.gotRationale)
	}
	if svc.gotActor != councilCaller {
		t.Error("the handler did not pass the authenticated caller to the service")
	}

	var body proposalBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.ID != "prop-1" {
		t.Errorf("id = %q, want the created proposal", body.ID)
	}
}

// The type is not validated in the handler: it is a domain rule with an
// execution branch behind it, so the service owns it and answers 400.
func TestProposalHandler_Create_ServiceValidationBecomes400(t *testing.T) {
	svc := &stubProposalService{err: service.ErrValidation}
	h := handler.NewProposalHandler(svc)

	req := withCouncil(httptest.NewRequest(http.MethodPost, "/api/v1/admin/proposals",
		strings.NewReader(`{"type":"council_exile","rationale":"begone"}`)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestProposalHandler_Create_RejectsUnknownFields(t *testing.T) {
	svc := &stubProposalService{view: sampleView()}
	h := handler.NewProposalHandler(svc)

	req := withCouncil(httptest.NewRequest(http.MethodPost, "/api/v1/admin/proposals",
		strings.NewReader(`{"type":"council_promotion","rationale":"x","status":"passed"}`)))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 — a caller must not be able to set the status", rec.Code)
	}
}

// --- Vote ---

func TestProposalHandler_Vote_ReturnsTheUpdatedProposal(t *testing.T) {
	view := sampleView()
	view.Status = domain.ProposalPassed
	view.ApproveCount = 3
	view.DecidedAt = decidedAt(time.Date(2026, 8, 14, 10, 0, 0, 0, time.UTC))
	svc := &stubProposalService{view: view}
	h := handler.NewProposalHandler(svc)

	req := withCouncil(httptest.NewRequest(http.MethodPost, "/api/v1/admin/proposals/prop-1/votes",
		strings.NewReader(`{"approve":true}`)))
	// chi supplies the URL parameter in production; the recorder needs it set
	// by hand because the handler is called directly.
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", "prop-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rec := httptest.NewRecorder()
	h.Vote(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if svc.gotVoteID != "prop-1" || !svc.gotApprove {
		t.Errorf("service got (%q, approve=%v), want (prop-1, true)", svc.gotVoteID, svc.gotApprove)
	}

	// The whole motion comes back, so the council member who cast the deciding
	// vote sees the outcome without reloading.
	var body proposalBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Status != "passed" || body.ApproveCount != 3 {
		t.Errorf("body = %+v, want the settled motion with its final tally", body)
	}
	if body.DecidedAt == "" {
		t.Error("a decided motion came back without decided_at")
	}
}

func TestProposalHandler_Vote_RejectIsPassedThrough(t *testing.T) {
	svc := &stubProposalService{view: sampleView()}
	h := handler.NewProposalHandler(svc)

	req := withCouncil(httptest.NewRequest(http.MethodPost, "/api/v1/admin/proposals/prop-1/votes",
		strings.NewReader(`{"approve":false}`)))
	rec := httptest.NewRecorder()
	h.Vote(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if svc.gotApprove {
		t.Error("approve=false arrived at the service as true")
	}
}

// Voting twice is ordinary user behaviour — a double-click or a stale tab — so
// it is a 400 and not a 500.
func TestProposalHandler_Vote_DuplicateIs400(t *testing.T) {
	svc := &stubProposalService{err: service.ErrValidation}
	h := handler.NewProposalHandler(svc)

	req := withCouncil(httptest.NewRequest(http.MethodPost, "/api/v1/admin/proposals/prop-1/votes",
		strings.NewReader(`{"approve":true}`)))
	rec := httptest.NewRecorder()
	h.Vote(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestProposalHandler_Vote_ErrorMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"unknown proposal", service.ErrNotFound, http.StatusNotFound},
		{"voting on your own removal", service.ErrForbidden, http.StatusForbidden},
		{"anything else", context.DeadlineExceeded, http.StatusInternalServerError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := handler.NewProposalHandler(&stubProposalService{err: tt.err})

			req := withCouncil(httptest.NewRequest(http.MethodPost, "/api/v1/admin/proposals/prop-1/votes",
				strings.NewReader(`{"approve":true}`)))
			rec := httptest.NewRecorder()
			h.Vote(rec, req)

			if rec.Code != tt.want {
				t.Errorf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestProposalHandler_Vote_UnauthenticatedIs401(t *testing.T) {
	h := handler.NewProposalHandler(&stubProposalService{view: sampleView()})

	rec := httptest.NewRecorder()
	h.Vote(rec, httptest.NewRequest(http.MethodPost, "/api/v1/admin/proposals/prop-1/votes",
		strings.NewReader(`{"approve":true}`)))

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}
