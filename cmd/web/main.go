package main

import (
	"github.com/fireynis/the-bell-api/cmd/web/processes/post"
	"github.com/labstack/echo/v4"
)

func main() {
	e := echo.New()
	post.Start(e)
}
