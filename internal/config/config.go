package config

import "github.com/caarlos0/env/v11"

type Config struct {
	Port             int    `env:"PORT" envDefault:"8080"`
	DatabaseURL      string `env:"DATABASE_URL,required"`
	RedisURL         string `env:"REDIS_URL,required"`
	KratosPublicURL  string `env:"KRATOS_PUBLIC_URL,required"`
	KratosAdminURL   string `env:"KRATOS_ADMIN_URL,required"`
	ImageStoragePath string `env:"IMAGE_STORAGE_PATH" envDefault:"/storage/the-bell/images"`
	TownName         string `env:"TOWN_NAME" envDefault:"My Town"`
}

func Load() (Config, error) {
	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
