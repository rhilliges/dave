package router

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

type testTemplate struct {
	location string
	contents string
}

func prepareTest(files []testTemplate) (*Router, func()) {
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
	fs := os.DirFS(dir)
	return NewRouter(fs), func() {
		os.RemoveAll(dir)
	}
}

// TODO:
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
// - CRUD
// - - POST - done
// - - PUT - done
// - - PATCH
// - - DELETE
// - SKIP_RESOLVER header (make configurable)
// - SKIP_GLOBAL_VALUES header (make configurable)
// - globals
// - - global available values
// - - global available functions (resolvers?)
// - - i18n
// - error handling
// - - logging (return error if some rendering failed)
// - - validation error during POST/PATCH/PUT
// - - fallback templates (unexpected error, auth error ...)
//
// FEATURES:
// - cache data to render template for quick browser refreshes
// - register path resolvers using reflection on the package path vs. a path variable
//
// EDGE CASES:
// - user writes to ResponseWriter -> panic and tell the user why not to do that
// - parse path variables before calling resolvers
// - make header case insensitive (double check if needed)
// - make configurable
// - - default layout
// - - default file extension
// - - always skip resolvers
// - how to integrate middleware? (authentication, authorization)

func TestRouter(t *testing.T) {
	templates := []testTemplate{
		{"test/index.tmpl", "test"},
		{"sub/test2/index.tmpl", "test2"},
		{"v1/{var1}/index.tmpl", "{{.path_variables.var1}}"},
		{"v1/{var1}/v2/{var2}/index.tmpl", "{{.path_variables.var1}},{{.path_variables.var2}}"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	testCases := []struct {
		path           string
		expectedRender string
	}{
		{"/test", "test"},
		{"/sub/test2", "test2"},
		{"/v1/value1", "value1"},
		{"/v1/value1/v2/value2", "value1,value2"},
	}
	for _, test := range testCases {
		t.Run("render "+test.path, func(t *testing.T) {
			req := httptest.NewRequest("GET", test.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			resp := rec.Result()
			body, _ := io.ReadAll(resp.Body)

			assert.Equal(t, test.expectedRender, string(body))
		})
	}
}

func TestRouter_ReferencingAnotherTemplate(t *testing.T) {
	templates := []testTemplate{
		{"path/to/another/template.tmpl", "T1"},
		{"path/with/template/index.tmpl", `{{template "path/to/another/template"}}`},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	req := httptest.NewRequest("GET", "/path/with/template", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "T1", string(body))
}

func TestRouter_TemplateHeader(t *testing.T) {
	templates := []testTemplate{
		{"path/to/create.tmpl", "create"},
		{"path/to/index.tmpl", "index"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	req := httptest.NewRequest("GET", "/path/to", nil)
	req.Header.Add("D-TEMPLATE", "create")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "create", string(body))
}

func TestRouter_DefaultLayout(t *testing.T) {
	templates := []testTemplate{
		{"layouts/default.tmpl", "layout-start {{if .content}} {{.content}} {{end}} layout-end"},
		{"path/to/index.tmpl", "layout-content"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	req := httptest.NewRequest("GET", "/path/to", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	resp := rec.Result()

	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "layout-start  layout-content  layout-end", string(body))
}

func TestRouter_LayoutHeader(t *testing.T) {
	templates := []testTemplate{
		{"layouts/custom.tmpl", "custom-layout-start {{if .content}} {{.content}} {{end}} custom-layout-end"},
		{"path/to/index.tmpl", "layout-content"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	req := httptest.NewRequest("GET", "/path/to", nil)
	rec := httptest.NewRecorder()
	req.Header.Add("D-LAYOUT", "custom")

	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "custom-layout-start  layout-content  custom-layout-end", string(body))
}

func TestRouter_GetResolver(t *testing.T) {
	templates := []testTemplate{
		{"v1/{var1}/v2/{var2}/index.tmpl", "{{.var1}},{{.path_variables.var2}}"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	req := httptest.NewRequest("GET", "/v1/value1/v2/value2", nil)
	rec := httptest.NewRecorder()
	resolverCalled := false

	router.UseResolver(
		"var1",
		Get(func(r *http.Request, value string) (any, error) {
			pathVariables := r.Context().Value(pathVariablesKey).(PathVariables)
			resolverCalled = true
			assert.Equal(t, "value1", value)
			assert.Equal(t, "value1", pathVariables["var1"])
			assert.Equal(t, "value2", pathVariables["var2"])
			return "resolvedValue", nil
		}),
	)

	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.True(t, resolverCalled, "resolver wasn't called")
	assert.Equal(t, "resolvedValue,value2", string(body))
}

func TestRouter_Post(t *testing.T) {
	templates := []testTemplate{
		{"v1/{var1}/path/index.tmpl", "{{.path_variables.var1}},{{.var1}}"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	handlerCalled := false
	router.UseResolver("var1",
		Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
			r.ParseForm()
			w.WriteHeader(202)
			assert.Equal(t, "value1", r.PostForm.Get("input1"))
			handlerCalled = true
			return "resolvedValue", nil
		}))

	data := url.Values{}
	data.Add("input1", "value1")
	req := httptest.NewRequest("POST", "/v1/val/path", strings.NewReader(data.Encode()))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Content-Length", strconv.Itoa(len(data.Encode())))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	resp := rec.Result()

	assert.True(t, handlerCalled, "POST handler wasn't called")
	assert.Equal(t, resp.StatusCode, 202)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "val,resolvedValue", string(body))
}

func TestRouter_Put(t *testing.T) {
	templates := []testTemplate{
		{"v1/{var1}/path/index.tmpl", "{{.path_variables.var1}},{{.var1}}"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	handlerCalled := false
	router.UseResolver("var1",
		Put(func(w http.ResponseWriter, r *http.Request) (any, error) {
			r.ParseForm()
			w.WriteHeader(202)
			assert.Equal(t, "value1", r.PostForm.Get("input1"))
			handlerCalled = true
			return "resolvedValue", nil
		}),
	)

	data := url.Values{}
	data.Add("input1", "value1")
	req := httptest.NewRequest("PUT", "/v1/val/path", strings.NewReader(data.Encode()))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Content-Length", strconv.Itoa(len(data.Encode())))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	resp := rec.Result()

	assert.True(t, handlerCalled, "PUT handler wasn't called")
	assert.Equal(t, resp.StatusCode, 202)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "val,resolvedValue", string(body))
}
