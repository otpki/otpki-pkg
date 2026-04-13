package spa

import (
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/samber/oops"
)

const ServiceName = "spa"

type memoryContent struct {
	Content []byte
	Size    int64
}

type memoryAsset struct {
	Path         string
	ContentType  string
	Uncompressed *memoryContent
	Brotli       *memoryContent
	Gzip         *memoryContent
}

func Handler(content fs.FS, spaPathPrefix, spaFallbackEntrypoint string) (http.HandlerFunc, error) {
	assetMap, err := loadAllAssets(content)
	if err != nil {
		return nil, err
	}

	fallbackAsset, fallbackExists := assetMap[spaFallbackEntrypoint]
	if !fallbackExists {
		return nil, oops.In(ServiceName).Errorf("SPA entrypoint %s not found", spaFallbackEntrypoint)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		requestPath := path.Clean(r.URL.Path)
		filePath := strings.TrimPrefix(requestPath, spaPathPrefix+"/")

		// see if the asset exists
		if asset, exists := assetMap[filePath]; exists {
			serveAsset(w, r, asset)
			return
		}

		// try fallback
		serveAsset(w, r, fallbackAsset)
	}, nil
}

func serveAsset(w http.ResponseWriter, r *http.Request, asset *memoryAsset) {
	acceptEncoding := r.Header.Get("Accept-Encoding")
	if asset.Brotli != nil && strings.Contains(acceptEncoding, "br") {
		serveMemoryContent(w, asset.Brotli, "br", asset.ContentType)
		return
	}

	if asset.Gzip != nil && strings.Contains(acceptEncoding, "gzip") {
		serveMemoryContent(w, asset.Gzip, "gzip", asset.ContentType)
		return
	}

	serveMemoryContent(w, asset.Uncompressed, "", asset.ContentType)
}

func serveMemoryContent(w http.ResponseWriter, content *memoryContent, contentEncoding, contentType string) {
	if contentEncoding != "" {
		w.Header().Set("Content-Encoding", contentEncoding)
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(content.Size, 10))

	_, _ = w.Write(content.Content)
}

func loadFileToMemory(content fs.FS, filename string) (*memoryContent, error) {
	file, err := content.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func(file fs.File) {
		_ = file.Close()
	}(file)

	fileContent, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	return &memoryContent{Content: fileContent, Size: int64(len(fileContent))}, nil
}

func loadAllAssets(content fs.FS) (map[string]*memoryAsset, error) {
	assetMap := make(map[string]*memoryAsset)

	// walk all files
	err := fs.WalkDir(content, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || p == "." {
			return nil
		}

		// skip compressed assets, they are handled during the main asset
		if strings.HasSuffix(p, ".gz") || strings.HasSuffix(p, ".br") {
			return nil
		}

		// load main asset
		uncompressed, err := loadFileToMemory(content, p)
		if err != nil {
			return oops.In(ServiceName).Wrapf(err, "loading asset %s", p)
		}

		asset := &memoryAsset{
			Path:         p,
			ContentType:  mime.TypeByExtension(filepath.Ext(p)),
			Uncompressed: uncompressed,
		}

		// attempt to load compressed versions
		asset.Brotli, _ = loadFileToMemory(content, p+".br")
		asset.Gzip, _ = loadFileToMemory(content, p+".gz")

		assetMap[p] = asset
		return nil
	})

	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "walking and loading embedded content")
	}
	return assetMap, nil
}
