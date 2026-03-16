package router

import (
	"context"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testTemplate struct {
	filepath string
	contents string
}

type templateTest struct {
	location string
	contents string
	// testTemplate
	path           string
	expectedRender string
}

type componentRef struct {
	location string
	name     string
	contents string
}

func createTestDir(files []templateTest) string {
	dir, err := os.MkdirTemp("", "template")
	if err != nil {
		log.Fatal(err)
	}
	for _, file := range files {
		filePath := filepath.Join(dir, file.location)
		filePathSegments := strings.Split(filePath, "/")
		fileDirPath := filepath.Join(filePathSegments[:len(filePathSegments)-1]...)
		err := os.MkdirAll("/"+fileDirPath, 0o777)
		if err != nil {
			log.Fatal(err)
		}
		f, err := os.Create(filePath)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		_, err = io.WriteString(f, file.contents)
		if err != nil {
			log.Fatal(err)
		}
	}
	return dir
}

func createTemplate(path, content string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.WriteString(f, content)
	if err != nil {
		return err
	}
	return nil
}

// TODO:
// - refactor tests (routerTest(router, ...))
// - file based routing - done
// - path variables - done
// - resolvers - done
// - components (nested templates) - done
// - TEMPLATE_NAME header - done
// - layouts
// - - default layout - done
// - - LAYOUT header - done
// - - deep-link layout file
// - - layout resolver (HX-Request header example)
// - SKIP_RESOLVER header (make configurable)
// - SKIP_GLOBAL_VALUES header (make configurable)
// - globals
// - - global available values
// - - global available functions (resolvers?)
// - - i18n
// - error handling
// - - logging (return error if some rendering failed)
// - - fallback templates (unexpected error, auth error ...)
// - PATCH/POST/UPDATE/DELETE
// - - "UsePoster/UsePatcher/UseUpdater" (different name ?)
// - - validation? (needed?)
//
// EDGE CASES:
// - make header case insensitive (double check if needed)
// - what about paths where a parent has an index.tmpl?
// - make configurable
// - - default layout
// - - default file extension
// - - always skip resolvers
// - how to integrate middleware (authentication, authorization)

func TestRouter(t *testing.T) {
	testCases := []templateTest{
		{"test/index.tmpl", "test", "/test", "test"},
		{"sub/test2/index.tmpl", "test2", "/sub/test2", "test2"},
		{"v1/{var1}/index.tmpl", "{{.var1}}", "/v1/value1", "value1"},
		{"v1/{var1}/v2/{var2}/index.tmpl", "{{.var1}},{{.var2}}", "/v1/value1/v2/value2", "value1,value2"},
	}
	dir := createTestDir(testCases)
	defer os.RemoveAll(dir)
	fs := os.DirFS(dir)
	router := NewRouter(fs)
	assert.NotNil(t, router, "router is nil")

	for _, test := range testCases {
		t.Run("render "+test.location, func(t *testing.T) {
			req := httptest.NewRequest("GET", test.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			resp := rec.Result()
			body, _ := io.ReadAll(resp.Body)

			assert.Equal(t, test.expectedRender, string(body))
		})
	}
}

func TestRouter_UseResolver(t *testing.T) {
	testCases := []templateTest{
		{"v1/{var1}/v2/{var2}/index.tmpl", "{{.var1}},{{.var2}}", "/v1/value1/v2/value2", "value1,value2"},
	}
	dir := createTestDir(testCases)
	defer os.RemoveAll(dir)
	fs := os.DirFS(dir)
	router := NewRouter(fs)
	req := httptest.NewRequest("GET", "/v1/value1/v2/value2", nil)
	rec := httptest.NewRecorder()
	resolverCalled := false
	router.Use("var1", func(ctx context.Context, value string) any {
		resolverCalled = true
		assert.Equal(t, "value1", value)
		return "resolvedValue"
	})
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.True(t, resolverCalled, "resolver wasn't called")
	assert.Equal(t, "resolvedValue,value2", string(body))
}

func TestRouter_ReferencingAnotherTemplate(t *testing.T) {
	templates := []templateTest{
		{"path/to/another/template.tmpl", "T1", "", ""},
		{"path/with/template/index.tmpl", `{{template "path/to/another/template"}}`, "", ""},
	}
	dir := createTestDir(templates)
	defer os.RemoveAll(dir)
	fs := os.DirFS(dir)
	router := NewRouter(fs)
	req := httptest.NewRequest("GET", "/path/with/template", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "T1", string(body))
}

func TestRouter_TemplateHeader(t *testing.T) {
	templates := []templateTest{
		{"path/to/create.tmpl", "create", "", ""},
		{"path/to/index.tmpl", "index", "", ""},
	}
	dir := createTestDir(templates)
	defer os.RemoveAll(dir)
	fs := os.DirFS(dir)
	router := NewRouter(fs)
	req := httptest.NewRequest("GET", "/path/to", nil)
	req.Header.Add("D-TEMPLATE", "create")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "create", string(body))
}

func TestRouter_DefaultLayout(t *testing.T) {
	templates := []templateTest{
		{"layouts/default.tmpl", "layout-start {{if .content}} {{.content}} {{end}} layout-end", "", ""},
		{"path/to/index.tmpl", "layout-content", "", ""},
	}
	dir := createTestDir(templates)
	defer os.RemoveAll(dir)
	fs := os.DirFS(dir)
	router := NewRouter(fs)
	req := httptest.NewRequest("GET", "/path/to", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	resp := rec.Result()

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "layout-start  layout-content  layout-end", string(body))
}

func TestRouter_LayoutHeader(t *testing.T) {
	templates := []templateTest{
		{"layouts/custom.tmpl", "custom-layout-start {{if .content}} {{.content}} {{end}} custom-layout-end", "", ""},
		{"path/to/index.tmpl", "layout-content", "", ""},
	}
	dir := createTestDir(templates)
	defer os.RemoveAll(dir)
	fs := os.DirFS(dir)
	router := NewRouter(fs)
	req := httptest.NewRequest("GET", "/path/to", nil)
	rec := httptest.NewRecorder()
	req.Header.Add("D-LAYOUT", "custom")
	router.ServeHTTP(rec, req)
	resp := rec.Result()

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "custom-layout-start  layout-content  custom-layout-end", string(body))
}
