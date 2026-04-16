package spa

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestHandlerWithConfigCachesImmutableAssets(t *testing.T) {
	handler, err := HandlerWithConfig(fstest.MapFS{
		"index.html": {
			Data: []byte("<html>ok</html>"),
		},
		"_app/immutable/app.js": {
			Data: []byte("console.log('ok')"),
		},
	}, Config{
		FallbackEntrypoint:    "index.html",
		ImmutableCacheControl: DefaultImmutableCacheControl,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/_app/immutable/app.js", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, DefaultImmutableCacheControl, rr.Header().Get("Cache-Control"))
	require.NotEmpty(t, rr.Header().Get("ETag"))
	require.Equal(t, "console.log('ok')", rr.Body.String())
}

func TestHandlerWithConfigFallbackDoesNotUseImmutableCacheControl(t *testing.T) {
	handler, err := HandlerWithConfig(fstest.MapFS{
		"index.html": {
			Data: []byte("<html>ok</html>"),
		},
		"_app/immutable/app.js": {
			Data: []byte("console.log('ok')"),
		},
	}, Config{
		FallbackEntrypoint:    "index.html",
		ImmutableCacheControl: DefaultImmutableCacheControl,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/dashboard", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Empty(t, rr.Header().Get("Cache-Control"))
	require.Equal(t, "<html>ok</html>", rr.Body.String())
}

func TestHandlerWithConfigReturnsNotModifiedOnETagMatch(t *testing.T) {
	handler, err := HandlerWithConfig(fstest.MapFS{
		"index.html": {
			Data: []byte("<html>ok</html>"),
		},
		"_app/immutable/app.js": {
			Data: []byte("console.log('ok')"),
		},
	}, Config{
		FallbackEntrypoint:    "index.html",
		ImmutableCacheControl: DefaultImmutableCacheControl,
	})
	require.NoError(t, err)

	firstReq := httptest.NewRequest(http.MethodGet, "/_app/immutable/app.js", nil)
	firstRR := httptest.NewRecorder()
	handler.ServeHTTP(firstRR, firstReq)

	etag := firstRR.Header().Get("ETag")
	require.NotEmpty(t, etag)

	secondReq := httptest.NewRequest(http.MethodGet, "/_app/immutable/app.js", nil)
	secondReq.Header.Set("If-None-Match", etag)
	secondRR := httptest.NewRecorder()
	handler.ServeHTTP(secondRR, secondReq)

	require.Equal(t, http.StatusNotModified, secondRR.Code)
	require.Empty(t, secondRR.Body.String())
}

func TestHandlerWithConfigSupportsHead(t *testing.T) {
	handler, err := HandlerWithConfig(fstest.MapFS{
		"index.html": {
			Data: []byte("<html>ok</html>"),
		},
		"_app/immutable/app.js": {
			Data: []byte("console.log('ok')"),
		},
	}, Config{
		FallbackEntrypoint:    "index.html",
		ImmutableCacheControl: DefaultImmutableCacheControl,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodHead, "/_app/immutable/app.js", nil)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "17", rr.Header().Get("Content-Length"))
	require.Empty(t, rr.Body.String())
}
