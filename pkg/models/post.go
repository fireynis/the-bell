package models

type Post struct {
	Base
	CityID string `json:"city_id"`
	UserID string `json:"user_id"`
	Title  string `json:"title"`
	Body   string `json:"body"`
}

type PagedPosts struct {
	QueryPost Post   `json:"query_post"`
	Posts     []Post `json:"posts"`
	Page      int    `json:"page"`
}
