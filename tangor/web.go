package main

import "github.com/go-martini/martini"

func nolog() *martini.ClassicMartini {
	r := martini.NewRouter()
	m := martini.New()
	m.Use(martini.Recovery())
	m.Use(martini.Static("public"))
	m.MapTo(r, (*martini.Routes)(nil))
	m.Action(r.Handle)
	return &martini.ClassicMartini{m, r}
}

func webui() {
	m := nolog()

	m.Get("/", func() string {
		return ""
	})

	m.Run()
}
