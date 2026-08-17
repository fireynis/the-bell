//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fireynis/the-bell/internal/app"
	"github.com/fireynis/the-bell/internal/domain"
	"github.com/fireynis/the-bell/internal/repository/postgres"
	"github.com/fireynis/the-bell/internal/testsupport"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The whole journey, against a real database and a real trust graph: a member
// invites somebody, the invitee looks the link up, signs in, and comes out the
// other side a member with a vouch edge and a role_history row.
//
// Redemption is driven through app.Deps.InviteService rather than through HTTP
// because it does not live behind a route — it runs inside the authenticating
// middleware, which this harness replaces with a mock identity.

type inviteEnv struct {
	deps    *app.Deps
	pool    *pgxpool.Pool
	inviter *domain.User
}

func newInviteEnv(t *testing.T, inviterRole domain.Role, trust float64) *inviteEnv {
	t.Helper()
	pool := testsupport.TestDB(t)
	inviter := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("inviter"), inviterRole, trust)
	return &inviteEnv{deps: testDeps(t, pool), pool: pool, inviter: inviter}
}

type createInviteBody struct {
	Invite struct {
		ID        string `json:"id"`
		Email     string `json:"email"`
		Status    string `json:"status"`
		ExpiresAt string `json:"expires_at"`
	} `json:"invite"`
	InviteURL  string `json:"invite_url"`
	EmailSent  bool   `json:"email_sent"`
	EmailError string `json:"email_error"`
}

// createInvite posts an invitation as the given member and returns the parsed
// response, failing the test on anything but 201.
func createInvite(t *testing.T, env *inviteEnv, as *domain.User, email, note string) createInviteBody {
	t.Helper()

	rec := doInviteRequest(t, env, as, http.MethodPost, "/api/v1/invites",
		`{"email":`+quote(email)+`,"note":`+quote(note)+`}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/v1/invites = %d, want 201: %s", rec.Code, rec.Body)
	}

	var body createInviteBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding the invitation: %v (%s)", err, rec.Body)
	}
	return body
}

func doInviteRequest(t *testing.T, env *inviteEnv, as *domain.User, method, path, payload string) *httptest.ResponseRecorder {
	t.Helper()

	srv := serverFromDeps(t, env.pool, env.deps, as)
	var reader *strings.Reader
	if payload != "" {
		reader = strings.NewReader(payload)
	} else {
		reader = strings.NewReader("")
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec
}

func quote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func tokenFromURL(t *testing.T, inviteURL string) string {
	t.Helper()
	_, token, found := strings.Cut(inviteURL, "invite=")
	if !found {
		t.Fatalf("no token in %q", inviteURL)
	}
	return token
}

func roleHistoryReasons(t *testing.T, pool *pgxpool.Pool, userID string) []string {
	t.Helper()

	rows, err := pool.Query(context.Background(),
		`SELECT reason FROM role_history WHERE user_id = $1 AND new_role = 'member' ORDER BY created_at`, userID)
	if err != nil {
		t.Fatalf("reading role_history: %v", err)
	}
	defer rows.Close()

	var reasons []string
	for rows.Next() {
		var reason string
		if err := rows.Scan(&reason); err != nil {
			t.Fatalf("scanning role_history: %v", err)
		}
		reasons = append(reasons, reason)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading role_history: %v", err)
	}
	return reasons
}

func TestInviteJourney_CreateLookUpAcceptBecomeAMember(t *testing.T) {
	env := newInviteEnv(t, domain.RoleMember, 80.0)
	ctx := context.Background()

	created := createInvite(t, env, env.inviter, "Newcomer@Example.com", "we met at the market")

	if created.Invite.Email != "newcomer@example.com" {
		t.Errorf("stored email = %q, want it lowercased", created.Invite.Email)
	}
	if created.Invite.Status != "open" {
		t.Errorf("status = %q, want open", created.Invite.Status)
	}
	// No SMTP in the harness, so the invitation still works and says so.
	if created.EmailSent {
		t.Error("email_sent = true with no relay configured")
	}
	if created.EmailError == "" {
		t.Error("email_error is empty; the member has no idea the mail did not go")
	}

	// The public lookup, as the registration page makes it: no session at all.
	token := tokenFromURL(t, created.InviteURL)
	anonymous := anonymousTestServer(t, env.pool, testsupport.TestRedis(t))
	rec := httptest.NewRecorder()
	anonymous.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/api/v1/invites/lookup?token="+token, nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("lookup = %d, want 200: %s", rec.Code, rec.Body)
	}
	var lookup struct {
		Email              string `json:"email"`
		TownName           string `json:"town_name"`
		InviterDisplayName string `json:"inviter_display_name"`
		Status             string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &lookup); err != nil {
		t.Fatalf("decoding the lookup: %v", err)
	}
	if lookup.Email != "newcomer@example.com" || lookup.Status != "open" {
		t.Errorf("lookup = %+v", lookup)
	}
	if strings.Contains(rec.Body.String(), token) {
		t.Error("the lookup response echoes the token back")
	}

	// The invitee signs up and signs in: a pending local user with that
	// address, which is what the auth middleware hands the redeemer.
	newcomer := testsupport.TestUser(t, env.pool, testsupport.UniqueKratosID("newcomer"), domain.RolePending, 50.0)
	if err := env.deps.InviteService.Redeem(ctx, newcomer, "newcomer@example.com"); err != nil {
		t.Fatalf("Redeem(): %v", err)
	}

	// A member, by way of a real vouch.
	stored, err := env.deps.UserService.GetByID(ctx, newcomer.ID)
	if err != nil {
		t.Fatalf("re-reading the newcomer: %v", err)
	}
	if stored.Role != domain.RoleMember {
		t.Fatalf("role = %q, want %q", stored.Role, domain.RoleMember)
	}

	vouches, err := env.deps.VouchService.ListReceivedVouches(ctx, newcomer.ID)
	if err != nil {
		t.Fatalf("listing received vouches: %v", err)
	}
	if len(vouches) != 1 || vouches[0].VoucherID != env.inviter.ID {
		t.Fatalf("received vouches = %+v, want one from the inviter", vouches)
	}

	// And the edge is in the graph, not merely the table — the trust score
	// depends on the graph, so a vouch that never became an edge is a vouch
	// that does not count.
	age := postgres.NewAGEQuerier(env.pool)
	vouchers, err := age.FindVouchersWithDepth(ctx, newcomer.ID, 1)
	if err != nil {
		t.Fatalf("FindVouchersWithDepth(): %v", err)
	}
	if _, found := vouchers[env.inviter.ID]; !found {
		t.Errorf("the trust graph has no edge from the inviter; vouchers = %v", vouchers)
	}

	// the-bell-brw: the promotion is in the audit trail, naming the vouch.
	reasons := roleHistoryReasons(t, env.pool, newcomer.ID)
	if len(reasons) != 1 {
		t.Fatalf("role_history rows = %v, want exactly one promotion to member", reasons)
	}
	if !strings.Contains(reasons[0], "vouch") {
		t.Errorf("role_history reason = %q, want it to name the vouch", reasons[0])
	}

	// The invitation now reads as accepted in the inviter's list, and still
	// carries no token.
	listed := doInviteRequest(t, env, env.inviter, http.MethodGet, "/api/v1/invites", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/invites = %d: %s", listed.Code, listed.Body)
	}
	if strings.Contains(listed.Body.String(), token) {
		t.Fatalf("the listing leaked the raw token: %s", listed.Body)
	}
	var list struct {
		Invites []struct {
			ID                    string `json:"id"`
			Status                string `json:"status"`
			ConsumedByDisplayName string `json:"consumed_by_display_name"`
		} `json:"invites"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &list); err != nil {
		t.Fatalf("decoding the listing: %v", err)
	}
	if len(list.Invites) != 1 || list.Invites[0].Status != "accepted" {
		t.Fatalf("listing = %+v, want one accepted invitation", list.Invites)
	}
}

// Redemption runs on every request a pending user makes, so it has to be safe
// to run repeatedly — and it must not produce a second vouch or a second audit
// row when the user is somehow still pending.
func TestInviteRedemptionIsIdempotent(t *testing.T) {
	env := newInviteEnv(t, domain.RoleMember, 80.0)
	ctx := context.Background()
	createInvite(t, env, env.inviter, "newcomer@example.com", "")

	newcomer := testsupport.TestUser(t, env.pool, testsupport.UniqueKratosID("newcomer"), domain.RolePending, 50.0)
	for i := 0; i < 3; i++ {
		if err := env.deps.InviteService.Redeem(ctx, newcomer, "newcomer@example.com"); err != nil {
			t.Fatalf("Redeem() attempt %d: %v", i+1, err)
		}
		// Force the pending case back, which is what a redemption that promoted
		// nobody would leave behind.
		newcomer.Role = domain.RolePending
	}

	vouches, err := env.deps.VouchService.ListReceivedVouches(ctx, newcomer.ID)
	if err != nil {
		t.Fatalf("listing received vouches: %v", err)
	}
	if len(vouches) != 1 {
		t.Errorf("received %d vouches, want exactly 1", len(vouches))
	}
	if reasons := roleHistoryReasons(t, env.pool, newcomer.ID); len(reasons) != 1 {
		t.Errorf("role_history rows = %v, want exactly 1", reasons)
	}
}

func TestInviteRedemptionIgnoresARevokedInvitation(t *testing.T) {
	env := newInviteEnv(t, domain.RoleMember, 80.0)
	ctx := context.Background()
	created := createInvite(t, env, env.inviter, "newcomer@example.com", "")

	rec := doInviteRequest(t, env, env.inviter, http.MethodDelete, "/api/v1/invites/"+created.Invite.ID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d, want 204: %s", rec.Code, rec.Body)
	}

	newcomer := testsupport.TestUser(t, env.pool, testsupport.UniqueKratosID("newcomer"), domain.RolePending, 50.0)
	if err := env.deps.InviteService.Redeem(ctx, newcomer, "newcomer@example.com"); err != nil {
		t.Fatalf("Redeem(): %v", err)
	}

	stored, err := env.deps.UserService.GetByID(ctx, newcomer.ID)
	if err != nil {
		t.Fatalf("re-reading the newcomer: %v", err)
	}
	if stored.Role != domain.RolePending {
		t.Errorf("role = %q, want the newcomer left pending", stored.Role)
	}

	// The lookup now refuses the token it accepted a moment ago, with the same
	// answer it gives a token that never existed.
	anonymous := anonymousTestServer(t, env.pool, testsupport.TestRedis(t))
	lookupRec := httptest.NewRecorder()
	anonymous.Handler().ServeHTTP(lookupRec, httptest.NewRequest(
		http.MethodGet, "/api/v1/invites/lookup?token="+tokenFromURL(t, created.InviteURL), nil))
	if lookupRec.Code != http.StatusNotFound {
		t.Errorf("lookup after revocation = %d, want 404", lookupRec.Code)
	}
}

// Somebody else's invitation must be indistinguishable from one that is not
// there, so this is a 404 and it leaves the invitation alone.
func TestInviteRevocationIsScopedToTheInviter(t *testing.T) {
	env := newInviteEnv(t, domain.RoleMember, 80.0)
	created := createInvite(t, env, env.inviter, "newcomer@example.com", "")
	stranger := testsupport.TestUser(t, env.pool, testsupport.UniqueKratosID("stranger"), domain.RoleMember, 80.0)

	rec := doInviteRequest(t, env, stranger, http.MethodDelete, "/api/v1/invites/"+created.Invite.ID, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE by a stranger = %d, want 404: %s", rec.Code, rec.Body)
	}

	// Still redeemable, which is the proof the refusal was not a partial write.
	newcomer := testsupport.TestUser(t, env.pool, testsupport.UniqueKratosID("newcomer"), domain.RolePending, 50.0)
	if err := env.deps.InviteService.Redeem(context.Background(), newcomer, "newcomer@example.com"); err != nil {
		t.Fatalf("Redeem(): %v", err)
	}
	stored, err := env.deps.UserService.GetByID(context.Background(), newcomer.ID)
	if err != nil {
		t.Fatalf("re-reading the newcomer: %v", err)
	}
	if stored.Role != domain.RoleMember {
		t.Errorf("role = %q, want the invitation to have survived the stranger's attempt", stored.Role)
	}
}

// The invitation is spent, the vouch is not made, and the newcomer stays
// pending with the ordinary paths open to them. Redemption must not report this
// as an error, or every subsequent request that person makes would log one.
func TestInviteRedemptionWhenTheInviterCanNoLongerVouch(t *testing.T) {
	env := newInviteEnv(t, domain.RoleMember, 80.0)
	ctx := context.Background()
	createInvite(t, env, env.inviter, "newcomer@example.com", "")

	// The inviter's standing collapses between sending the invitation and its
	// being accepted.
	userRepo := postgres.NewUserRepo(postgres.New(env.pool))
	if err := userRepo.UpdateUserTrustScore(ctx, env.inviter.ID, 20.0); err != nil {
		t.Fatalf("dropping the inviter's trust: %v", err)
	}

	newcomer := testsupport.TestUser(t, env.pool, testsupport.UniqueKratosID("newcomer"), domain.RolePending, 50.0)
	if err := env.deps.InviteService.Redeem(ctx, newcomer, "newcomer@example.com"); err != nil {
		t.Fatalf("Redeem() = %v, want nil: a worthless invitation is not a failure", err)
	}

	stored, err := env.deps.UserService.GetByID(ctx, newcomer.ID)
	if err != nil {
		t.Fatalf("re-reading the newcomer: %v", err)
	}
	if stored.Role != domain.RolePending {
		t.Errorf("role = %q, want the newcomer left pending", stored.Role)
	}
	vouches, err := env.deps.VouchService.ListReceivedVouches(ctx, newcomer.ID)
	if err != nil {
		t.Fatalf("listing received vouches: %v", err)
	}
	if len(vouches) != 0 {
		t.Errorf("received %d vouches from a collapsed inviter, want none", len(vouches))
	}

	// Spent, though: the invitation cannot be tried again.
	if err := env.deps.InviteService.Redeem(ctx, newcomer, "newcomer@example.com"); err != nil {
		t.Fatalf("second Redeem(): %v", err)
	}
	listed := doInviteRequest(t, env, env.inviter, http.MethodGet, "/api/v1/invites", "")
	if !strings.Contains(listed.Body.String(), `"accepted"`) {
		t.Errorf("the invitation does not read as accepted: %s", listed.Body)
	}
}

// Creating an invitation spends the same allowance vouching does, and the
// service — not the HTTP limiter — is what enforces it.
func TestInviteBudgetIsSharedWithVouching(t *testing.T) {
	env := newInviteEnv(t, domain.RoleMember, 80.0)
	ctx := context.Background()

	// Two vouches given today leave one of the three-a-day allowance.
	for i, name := range []string{"vouchee-a", "vouchee-b"} {
		vouchee := testsupport.TestUser(t, env.pool, testsupport.UniqueKratosID(name), domain.RolePending, 50.0)
		if _, err := env.deps.VouchService.Vouch(ctx, env.inviter.ID, vouchee.ID); err != nil {
			t.Fatalf("vouch %d: %v", i, err)
		}
	}

	createInvite(t, env, env.inviter, "third@example.com", "")

	rec := doInviteRequest(t, env, env.inviter, http.MethodPost, "/api/v1/invites",
		`{"email":"fourth@example.com"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("fourth endorsement of the day = %d, want 400: %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "allowance") {
		t.Errorf("body = %s, want it to explain the shared allowance", rec.Body)
	}
}

// Council is exempt, mirroring the unlimited approvals decision: in invite mode
// the council's invitations are how a town gets populated at all.
func TestCouncilInvitesAreNotRationed(t *testing.T) {
	env := newInviteEnv(t, domain.RoleCouncil, 100.0)

	for i := 0; i < 5; i++ {
		createInvite(t, env, env.inviter, councilEmail(i), "")
	}
}

func councilEmail(i int) string {
	return "resident" + string(rune('a'+i)) + "@example.com"
}

// Accepting an invitation must not be charged to the inviter's allowance a
// second time — the budget was spent when the invitation was created, and the
// invitee chooses the day they accept.
func TestRedemptionDoesNotSpendTheInvitersDailyAllowance(t *testing.T) {
	env := newInviteEnv(t, domain.RoleMember, 80.0)
	ctx := context.Background()

	// All three of today's allowance go out as invitations.
	emails := []string{"one@example.com", "two@example.com", "three@example.com"}
	for _, email := range emails {
		createInvite(t, env, env.inviter, email, "")
	}

	// All three are accepted the same day. Under the ordinary vouch limit the
	// third would be refused, which would leave a newcomer stranded through no
	// fault of anybody's.
	for i, email := range emails {
		newcomer := testsupport.TestUser(t, env.pool,
			testsupport.UniqueKratosID("newcomer"), domain.RolePending, 50.0)
		if err := env.deps.InviteService.Redeem(ctx, newcomer, email); err != nil {
			t.Fatalf("redeeming invitation %d: %v", i+1, err)
		}
		stored, err := env.deps.UserService.GetByID(ctx, newcomer.ID)
		if err != nil {
			t.Fatalf("re-reading newcomer %d: %v", i+1, err)
		}
		if stored.Role != domain.RoleMember {
			t.Errorf("newcomer %d role = %q, want %q", i+1, stored.Role, domain.RoleMember)
		}
	}
}

// the-bell-brw, the other promotion path: council approval during bootstrap.
func TestCouncilApprovalIsRecordedInRoleHistory(t *testing.T) {
	pool := testsupport.TestDB(t)
	ctx := context.Background()
	deps := testDeps(t, pool)

	if err := deps.ConfigRepo.SetTownConfig(ctx, "bootstrap_mode", "true"); err != nil {
		t.Fatalf("entering bootstrap mode: %v", err)
	}
	applicant := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("applicant"), domain.RolePending, 50.0)
	councillor := testsupport.TestUser(t, pool, testsupport.UniqueKratosID("councillor"), domain.RoleCouncil, 100.0)

	// Through the route, because that is the only way in: the approval service
	// is reachable through HTTP and app.Deps deliberately does not re-export it.
	srv := serverFromDeps(t, pool, deps, councillor)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodPost, "/api/v1/vouches/approve/"+applicant.ID, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/v1/vouches/approve = %d, want 200: %s", rec.Code, rec.Body)
	}

	reasons := roleHistoryReasons(t, pool, applicant.ID)
	if len(reasons) != 1 {
		t.Fatalf("role_history rows = %v, want exactly one promotion to member", reasons)
	}
	if !strings.Contains(reasons[0], "council approval") {
		t.Errorf("role_history reason = %q, want it to name the approval", reasons[0])
	}
}
