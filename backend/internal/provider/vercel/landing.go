package vercel

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
)

// This is the reviewed public design allowlist, never an arbitrary server path.
//
//go:embed all:landing/files
var landingFiles embed.FS

type LandingArtifact struct {
	Name       string `json:"name"`
	FileCount  int    `json:"fileCount"`
	Bytes      int    `json:"bytes"`
	SHA256     string `json:"sha256"`
	PreviewURL string `json:"previewUrl"`
}

func Landing() (LandingArtifact, []StaticFile, error) {
	root, err := fs.Sub(landingFiles, "landing/files")
	if err != nil {
		return LandingArtifact{}, nil, err
	}
	files := []StaticFile{}
	h := sha256.New()
	info := LandingArtifact{Name: "Cooker landing", PreviewURL: "/landing-preview/"}
	err = fs.WalkDir(root, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(root, path)
		if err != nil {
			return err
		}
		files = append(files, StaticFile{Path: path, Data: data})
		fileHash := sha256.Sum256(data)
		h.Write([]byte(path))
		h.Write([]byte{0})
		h.Write(fileHash[:])
		info.Bytes += len(data)
		return nil
	})
	info.FileCount = len(files)
	info.SHA256 = hex.EncodeToString(h.Sum(nil))
	return info, files, err
}

// Public preview exposes only embedded publication bytes, with no account data.
func LandingPreview() http.Handler {
	root, _ := fs.Sub(landingFiles, "landing/files")
	return http.StripPrefix("/landing-preview/", http.FileServer(http.FS(root)))
}
