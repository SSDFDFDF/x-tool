package admin

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed all:adminui
var adminAssets embed.FS

func FileServer() http.Handler {
	sub, err := fs.Sub(adminAssets, "adminui")
	if err != nil {
		panic(err)
	}
	return http.FileServer(http.FS(sub))
}
