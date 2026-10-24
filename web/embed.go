package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed *.html css/* js/*
var content embed.FS

// GetFileSystem 返回可用於 http.FileServer 的嵌入式檔案系統
func GetFileSystem() (http.FileSystem, error) {
	// 如果需要解套層級可以直接使用 fs.Sub
	sub, err := fs.Sub(content, ".")
	if err != nil {
		return nil, err
	}
	return http.FS(sub), nil
}
