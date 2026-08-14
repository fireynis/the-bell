package kratos

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	client "github.com/ory/kratos-client-go"
)

const (
	// identitySchemaID is the Kratos identity schema new accounts are created under.
	identitySchemaID = "default"
	// identityStateActive marks a created identity as usable immediately.
	identityStateActive = "active"
	// passwordEntropyBytes is the size of a generated password before hex encoding.
	passwordEntropyBytes = 32

	// requestTimeout bounds a single Admin API call. The SDK's default client
	// has no timeout at all, so a Kratos that accepts the connection and then
	// stalls would hang `bell setup` forever with no way out but Ctrl-C.
	// Identity creation is one small write against a service that is normally
	// a container away, so 30s is far longer than a healthy call needs while
	// still guaranteeing the command returns.
	requestTimeout = 30 * time.Second
)

// AdminClient wraps the Kratos Admin API for identity management.
type AdminClient struct {
	api client.IdentityAPI
}

// newConfiguration builds the SDK configuration for the Admin API. The timeout
// is a parameter so the wiring can be exercised without waiting out the
// production budget.
func newConfiguration(adminURL string, timeout time.Duration) *client.Configuration {
	cfg := client.NewConfiguration()
	cfg.Servers = client.ServerConfigurations{{URL: adminURL}}
	cfg.HTTPClient = &http.Client{Timeout: timeout}
	return cfg
}

// NewAdminClient creates an AdminClient pointing at the given Kratos Admin URL.
func NewAdminClient(adminURL string) *AdminClient {
	apiClient := client.NewAPIClient(newConfiguration(adminURL, requestTimeout))
	return &AdminClient{api: apiClient.IdentityAPI}
}

// generatePassword returns a random hex password.
//
// It reads from crypto/rand, never math/rand: until the user completes a
// recovery flow this string is the account's only credential, so it must not be
// derivable from the process start time or any other observable seed.
func generatePassword() (string, error) {
	b := make([]byte, passwordEntropyBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating random password: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// newIdentityBody builds the create-identity request for a new account: the
// traits the identity schema expects, a password credential, and the active
// state that lets the account sign in without a further activation step.
func newIdentityBody(email, displayName, password string) *client.CreateIdentityBody {
	body := client.NewCreateIdentityBody(identitySchemaID, map[string]interface{}{
		"email": email,
		"name":  displayName,
	})

	pwConfig := client.NewIdentityWithCredentialsPasswordConfig()
	pwConfig.SetPassword(password)
	pw := client.NewIdentityWithCredentialsPassword()
	pw.SetConfig(*pwConfig)
	creds := client.NewIdentityWithCredentials()
	creds.SetPassword(*pw)

	body.SetCredentials(*creds)
	body.SetState(identityStateActive)
	return body
}

// DisplayNameFromTraits reads the `name` trait off a decoded identity trait set.
//
// Traits arrive as decoded JSON — the SDK types them as interface{} and the
// identity schema is per-deployment — so every step is a checked assertion: a
// schema without `name`, or with a `name` that is not a string, yields "" and a
// caller that has no name to apply. It is never an error, because a deployment
// whose schema names the field differently is a configuration the app has to
// keep running under, not a failure to report.
func DisplayNameFromTraits(traits interface{}) string {
	m, ok := traits.(map[string]interface{})
	if !ok {
		return ""
	}
	name, _ := m["name"].(string)
	return strings.TrimSpace(name)
}

// IdentityDisplayName fetches one identity by ID and returns its `name` trait.
//
// An identity that exists but carries no usable name yields ("", nil): the
// caller cannot tell that apart from a schema mismatch and does not need to,
// since both mean "nothing to write". Only the fetch itself errors.
func (c *AdminClient) IdentityDisplayName(ctx context.Context, kratosID string) (string, error) {
	identity, _, err := c.api.GetIdentity(ctx, kratosID).Execute()
	if err != nil {
		return "", fmt.Errorf("kratos get identity %s: %w", kratosID, err)
	}
	return DisplayNameFromTraits(identity.GetTraits()), nil
}

// CreateIdentity creates a Kratos identity with password credentials.
// If password is empty, a random 32-byte password is generated (the user
// can reset via recovery flow).
// Returns the Kratos identity ID and the password that was used.
func (c *AdminClient) CreateIdentity(ctx context.Context, email, displayName, password string) (kratosID string, usedPassword string, err error) {
	if password == "" {
		generated, err := generatePassword()
		if err != nil {
			return "", "", err
		}
		password = generated
	}

	body := newIdentityBody(email, displayName, password)

	identity, _, err := c.api.CreateIdentity(ctx).CreateIdentityBody(*body).Execute()
	if err != nil {
		return "", "", fmt.Errorf("kratos create identity: %w", err)
	}

	return identity.GetId(), password, nil
}
