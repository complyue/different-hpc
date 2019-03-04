package main

import (
	"net/http"

	"github.com/complyue/different-hpc/pkg/bknd"
	"github.com/flosch/pongo2"
	"github.com/gorilla/mux"
)

func definePageRoutes(router *mux.Router) {

	router.Handle("/", &bknd.Pongo2Page{
		TmplFile: "web/templates/index.html",
		UpdateCtx: func(ctx pongo2.Context, r *http.Request) {
			ctx["title"] = "Different HPC"
		},
	})

}
