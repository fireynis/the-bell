package post

import (
	"fmt"
	"github.com/labstack/echo/v4"
	"net/http"
)

// e.GET("/post/:id", getUser)
func (p *Process) getPost(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return fmt.Errorf("id is empty")
	}
	return c.String(http.StatusOK, id)
}
