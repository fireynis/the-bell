package kratos

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	client "github.com/ory/kratos-client-go"
)

// AdminClient wraps the Kratos Admin API for identity management.
type AdminClient struct {
	api client.IdentityAPI
}

// NewAdminClient creates an AdminClient pointing at the given Kratos Admin URL.
func NewAdminClient(adminURL string) *AdminClient {
	cfg := client.NewConfiguration()
	cfg.Servers = client.ServerConfigurations{{URL: adminURL}}
	apiClient := client.NewAPIClient(cfg)
	return &AdminClient{api: apiClient.IdentityAPI}
}

// CreateIdentity creates a Kratos identity with password credentials.
// If password is empty, a random 32-byte password is generated (the user
// can reset via recovery flow).
// Returns the Kratos identity ID and the password that was used.
func (c *AdminClient) CreateIdentity(ctx context.Context, email, displayName, password string) (kratosID string, usedPassword string, err error) {
	if password == "" {
		b := make([]byte, 32)
		if _, err := rand.Read(b); err != nil {
			return "", "", fmt.Errorf("generating random password: %w", err)
		}
		password = hex.EncodeToString(b)
	}

	body := client.NewCreateIdentityBody("default", map[string]interface{}{
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
	body.SetState("active")

	identity, _, err := c.api.CreateIdentity(ctx).CreateIdentityBody(*body).Execute()
	if err != nil {
		return "", "", fmt.Errorf("kratos create identity: %w", err)
	}

	return identity.GetId(), password, nil
}
