package bknd

import (
	"github.com/gorilla/mux"
)

func DefineApiRoutes(router *mux.Router) {

	// http route to pixiecore API
	router.HandleFunc("/pixie/v1/boot/{mac}", pixieApi)

}
