package bknd

import (
	"github.com/gorilla/mux"
)

func DefineHttpRoutes(router *mux.Router) {

	// http route to pixiecore API
	router.HandleFunc("/pixie/v1/boot/{mac}", pixieApi)

}
