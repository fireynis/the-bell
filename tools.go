//go:build tools

package tools

import (
	_ "github.com/caarlos0/env/v11"
	_ "github.com/go-chi/chi/v5"
	_ "github.com/google/uuid"
	_ "github.com/jackc/pgx/v5"
	_ "github.com/ory/kratos-client-go"
	_ "github.com/pressly/goose/v3"
	_ "github.com/redis/go-redis/v9"
)
