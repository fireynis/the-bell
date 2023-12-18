package helpers

import (
	"database/sql"
)

var connections map[string]*sql.DB

func CreateConnection(dsn string, name string) (*sql.DB, error) {
	if _, ok := connections[name]; ok {
		return connections[name], nil
	}
	var err error
	connection, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	connections[name] = connection
	return connection, nil
}
