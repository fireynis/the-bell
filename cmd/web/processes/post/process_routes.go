package post

func (p *Process) initRoutes() {
	p.router.GET("/posts/:id", p.getPost)
}
