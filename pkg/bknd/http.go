package bknd

import (
	"net/http"

	"github.com/flosch/pongo2"
	"github.com/gorilla/mux"
)

var DevMode bool

type Pongo2Page struct {
	TmplFile    string
	UpdateCtx   func(ctx pongo2.Context, r *http.Request)
	HideMuxVars bool
	tmpl        *pongo2.Template
}

// implementing http.Handler
func (page *Pongo2Page) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if page.tmpl == nil || DevMode {
		page.tmpl = pongo2.Must(pongo2.FromFile(page.TmplFile))
	}
	ctx := make(pongo2.Context)
	if !page.HideMuxVars {
		for k, v := range mux.Vars(r) {
			ctx[k] = v
		}
	}
	if page.UpdateCtx != nil {
		page.UpdateCtx(ctx, r)
	}
	err := page.tmpl.ExecuteWriter(ctx, w)
	if err != nil {
		panic(err)
	}
}
