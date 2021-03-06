package bknd

import (
	"net/http"

	"github.com/complyue/different-hpc/pkg/ccm"

	"github.com/flosch/pongo2"
	"github.com/gorilla/mux"
)

func DefinePageRoutes(router *mux.Router) {

	router.Handle("/", &Pongo2Page{
		TmplFile: "web/templates/index.html",
		UpdateCtx: func(ctx pongo2.Context, r *http.Request) {
			ctx["title"] = "Different HPC Control Center"

			pulseCfg := ccm.GetPulseCfg()
			ctx["sshUser"] = pulseCfg.SshUser

			ccm.GetComputeNodeCfgs()
			ctx["cnips"] = ccm.ListCaredIPs()
		},
	})

}
