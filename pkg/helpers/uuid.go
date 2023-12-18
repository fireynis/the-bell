package helpers

import "github.com/google/uuid"

func GenerateUUID() string {
	uuid, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return uuid.String()
}
