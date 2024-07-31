package helpers

import "github.com/google/uuid"

func GenerateTSUUID() string {
	newV7, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return newV7.String()
}

func GenerateUUID() string {
	newUUID, err := uuid.NewUUID()
	if err != nil {
		panic(err)
	}
	return newUUID.String()
}
