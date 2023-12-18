package post

import (
	"github.com/fireynis/the-bell-api/pkg/contracts/post/post_services"
	"github.com/labstack/echo/v4"
)

type Config struct {
	readerName string `env:"READER_DB_NAME"`
	writerName string `env:"WRITER_DB_NAME"`
}

type Process struct {
	readerService post_services.ReaderService
	writerService post_services.WriterService
	router        *echo.Echo
	Config
}

func (p *Process) Initialize() {
	p.initRoutes()
}

func Start(router *echo.Echo) {
	p := &Process{
		router: router,
	}
	p.Initialize()
}
