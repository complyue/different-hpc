package bknd

import (
	"github.com/gorilla/mux"
)

func DefineApiRoutes(router *mux.Router) {

	// http route to pixiecore API
	router.HandleFunc("/pixie/v1/boot/{mac}", pixieApi)

	// http route to compute node API
	router.HandleFunc("/cnode/v1/save", cnodeSaveCfg)

}
