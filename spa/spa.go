package spa

import (
	"crypto/sha256"
	"encoding/hex"
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

const (
	ServiceName                  = "spa"
	DefaultImmutableCacheControl = "public, max-age=31536000, immutable"
)

type Config struct {
	PathPrefix            string
	FallbackEntrypoint    string
	ImmutableCacheControl string
	ImmutablePathPrefixes []string
}

type memoryContent struct {
	Content    []byte
	Size       int64
	SizeHeader string
	ETag       string
}

type memoryAsset struct {
	Path                  string
	ContentType           string
	CacheControl          string
	HasCompressedVariants bool
	Uncompressed          *memoryContent
	Brotli                *memoryContent
	Gzip                  *memoryContent
}

func Handler(content fs.FS, spaPathPrefix, spaFallbackEntrypoint string) (http.HandlerFunc, error) {
	return HandlerWithConfig(content, Config{
		PathPrefix:         spaPathPrefix,
		FallbackEntrypoint: spaFallbackEntrypoint,
	})
}

func HandlerWithConfig(content fs.FS, config Config) (http.HandlerFunc, error) {
	config = config.withDefaults()

	assetMap, err := loadAllAssets(content, config)
	if err != nil {
		return nil, err
	}

	fallbackAsset, fallbackExists := assetMap[config.FallbackEntrypoint]
	if !fallbackExists {
		return nil, oops.In(ServiceName).Errorf("SPA entrypoint %s not found", config.FallbackEntrypoint)
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", http.MethodGet+", "+http.MethodHead)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		requestPath := path.Clean(r.URL.Path)
		filePath := assetPath(requestPath, config.PathPrefix)

		// see if the asset exists
		if asset, exists := assetMap[filePath]; exists {
			serveAsset(w, r, asset)
			return
		}

		// try fallback
		serveAsset(w, r, fallbackAsset)
	}, nil
}

func (c Config) withDefaults() Config {
	if c.PathPrefix != "" && !strings.HasPrefix(c.PathPrefix, "/") {
		c.PathPrefix = "/" + c.PathPrefix
	}

	c.PathPrefix = strings.TrimRight(c.PathPrefix, "/")
	if c.PathPrefix == "/" {
		c.PathPrefix = ""
	}

	if c.ImmutableCacheControl == "" && len(c.ImmutablePathPrefixes) > 0 {
		c.ImmutableCacheControl = DefaultImmutableCacheControl
	}

	if c.ImmutableCacheControl != "" && len(c.ImmutablePathPrefixes) == 0 {
		c.ImmutablePathPrefixes = []string{"_app/immutable"}
	}

	normalizedPrefixes := make([]string, 0, len(c.ImmutablePathPrefixes))
	for _, prefix := range c.ImmutablePathPrefixes {
		prefix = strings.Trim(strings.TrimSpace(prefix), "/")
		if prefix != "" {
			normalizedPrefixes = append(normalizedPrefixes, prefix)
		}
	}
	c.ImmutablePathPrefixes = normalizedPrefixes

	return c
}

func assetPath(requestPath, pathPrefix string) string {
	if pathPrefix == "" {
		return strings.TrimPrefix(requestPath, "/")
	}

	return strings.TrimPrefix(requestPath, pathPrefix+"/")
}

func serveAsset(w http.ResponseWriter, r *http.Request, asset *memoryAsset) {
	content := asset.Uncompressed
	contentEncoding := ""

	if asset.HasCompressedVariants {
		w.Header().Set("Vary", "Accept-Encoding")

		acceptEncoding := r.Header.Get("Accept-Encoding")
		if asset.Brotli != nil && strings.Contains(acceptEncoding, "br") {
			content = asset.Brotli
			contentEncoding = "br"
		} else if asset.Gzip != nil && strings.Contains(acceptEncoding, "gzip") {
			content = asset.Gzip
			contentEncoding = "gzip"
		}
	}

	serveMemoryContent(w, r, content, contentEncoding, asset)
}

func serveMemoryContent(w http.ResponseWriter, r *http.Request, content *memoryContent, contentEncoding string, asset *memoryAsset) {
	if asset.CacheControl != "" {
		w.Header().Set("Cache-Control", asset.CacheControl)
	}

	w.Header().Set("ETag", content.ETag)
	if etagMatches(r.Header.Get("If-None-Match"), content.ETag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	if contentEncoding != "" {
		w.Header().Set("Content-Encoding", contentEncoding)
	}

	w.Header().Set("Content-Type", asset.ContentType)
	w.Header().Set("Content-Length", content.SizeHeader)

	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}

	_, _ = w.Write(content.Content)
}

func etagMatches(ifNoneMatch, etag string) bool {
	if ifNoneMatch == "" {
		return false
	}

	if strings.TrimSpace(ifNoneMatch) == "*" {
		return true
	}

	for candidate := range strings.SplitSeq(ifNoneMatch, ",") {
		candidate = strings.TrimSpace(candidate)
		candidate = strings.TrimPrefix(candidate, "W/")
		if candidate == etag {
			return true
		}
	}

	return false
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

	sum := sha256.Sum256(fileContent)

	return &memoryContent{
		Content:    fileContent,
		Size:       int64(len(fileContent)),
		SizeHeader: strconv.Itoa(len(fileContent)),
		ETag:       "\"" + hex.EncodeToString(sum[:]) + "\"",
	}, nil
}

func loadAllAssets(content fs.FS, config Config) (map[string]*memoryAsset, error) {
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

		contentType := mime.TypeByExtension(filepath.Ext(p))
		if contentType == "" {
			contentType = "application/octet-stream"
		}

		asset := &memoryAsset{
			Path:         p,
			ContentType:  contentType,
			CacheControl: cacheControlForPath(p, config),
			Uncompressed: uncompressed,
		}

		// attempt to load compressed versions
		asset.Brotli, _ = loadFileToMemory(content, p+".br")
		asset.Gzip, _ = loadFileToMemory(content, p+".gz")
		asset.HasCompressedVariants = asset.Brotli != nil || asset.Gzip != nil

		assetMap[p] = asset
		return nil
	})
	if err != nil {
		return nil, oops.In(ServiceName).Wrapf(err, "walking and loading embedded content")
	}
	return assetMap, nil
}

func cacheControlForPath(assetPath string, config Config) string {
	if config.ImmutableCacheControl == "" {
		return ""
	}

	for _, prefix := range config.ImmutablePathPrefixes {
		if assetPath == prefix || strings.HasPrefix(assetPath, prefix+"/") {
			return config.ImmutableCacheControl
		}
	}

	return ""
}
