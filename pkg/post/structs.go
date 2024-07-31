package post

import (
	bellErrors "github.com/fireynis/the-bell-api/pkg/errors"
	"github.com/fireynis/the-bell-api/pkg/helpers"
)

type Post struct {
	helpers.Base
	CityID string `json:"city_id"`
	UserID string `json:"user_id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

func (p *Post) Validate() error {
	errors := make(map[string]string)
	if p.Title == "" {
		errors["title"] = "title is required"
	}
	if p.Body == "" {
		errors["body"] = "body is required"
	}
	if p.CityID == "" {
		errors["city_id"] = "city_id is required"
	}
	if p.UserID == "" {
		errors["user_id"] = "user_id is required"
	}

	if len(errors) > 0 {
		return bellErrors.ValidationError{ValidationErrors: errors}
	}
	return nil
}

type QueryParams struct {
	Page    int
	UserID  string
	UserIDs []string
	CityID  string
	CityIDs []string
}
