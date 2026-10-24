package api

import (
	"context"
	"just-nas/internal/backup"
	"net/http"
	"time"
)

func (r *Router) restoreDownload(w http.ResponseWriter, q *http.Request) {
	if q.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	ds := q.URL.Query().Get("dataset")
	if q.URL.Path == "/api/backup/restores" {
		folders, e := backup.RestoreFolders(ds)
		if e != nil {
			jsonResponse(w, 400, map[string]string{"error": e.Error()})
			return
		}
		jsonResponse(w, 200, folders)
		return
	}
	name := q.URL.Query().Get("name")
	root, e := backup.ValidateRestoreDownload(ds, name)
	if e != nil {
		jsonResponse(w, 400, map[string]string{"error": e.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Content-Disposition", "attachment; filename="+name+".tar")
	ctx, cancel := context.WithTimeout(q.Context(), 30*time.Minute)
	defer cancel()
	if e = backup.StreamRestore(ctx, root, name, w); e != nil {
		panic(http.ErrAbortHandler)
	}
}
