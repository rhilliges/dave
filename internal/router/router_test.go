package router

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"log/slog"
	"mime/multipart"
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

// FEATURES:
// - file based routing
// - path variables
// - resolvers
// - CRUD
// - - POST
// - - PUT
// - - PATCH
// - - DELETE
// - components (nested templates)
// - TEMPLATE_NAME header
// - layouts
// - - default layout
// - - LAYOUT header
// - - layout resolvers (HX-Request header example, D-LAYOUT default implementation)
// - globals
// - template functions
// - error handling
// - - logging (log unexpected errors if some rendering failed)
// - - validation error during POST/PATCH/PUT (use HX-Location header)
// - - redirect error (use HX-Location header)
// - - fallback templates (unexpected error, not found)
// - content response headers (html, text)
// - d_form_handler header
// - cache data to render template for quick browser refreshes
// - logging (log unexpected errors if some rendering failed) -> add/remove "DAVE" context variable
// - user writes to ResponseWriter -> log error
// - make configurable
// - - default layout
// - - default file extension

//
// DOCUMENTATION:
// - what is this project (easy to use w/ HTMX, great DX)
// - how to use is (step by step instructions on how to build an app)
// - - start with simple index file (some app with a table)
// - - add a global + use it in the template file
// - - introduce a path variable + load load data using path variable plus a global
// - - using func to format dates
// - - use form handlers + HX-Location to redirect when entity was created/updated/removed
// - - use template header to e.g. show dialogs
// - - use layout header to add a fullscreen mode
// - - helpful links
// - - - HTMX implementation patterns
// - - - hyperscript for client side stuff
// - - - AlpineJS
// - request lifecycle
// - template priority
// - layout priority
// - only write error response codes, writing body throws error
// - globals, why, how and when to use them
// - template functions why, how and when to use them
// - - i18n example implementation (load translation file + add template func to get value based on key)
// - reference for available headers
// - how to use HX-Location for after creating an entity is successful
// - document router scanTemplates function (startup vs first request vs dev mode behaviour)
// - route conflict resolution
// - user writes to ResponseWriter
// - document all Conf options
// - document form parsing behavior
// - developer experience
// - - what does dev mode do?
// - - how to auto reload using air (don't recompile go code if only templates change)

// What to do next:
// - dev experience (caching, scanTemplates)
// - clone root templates before rendering
// - custom fallback templates for e.g. auth errors
// - layout resolvers (HX-Request header example, D-LAYOUT default implementation)
// - figure out middlewares
// - how to integrate middleware? (authentication, authorization)
// - register path resolvers using reflection on the package path vs. a path variable - see if feasible
// - custom renderer

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

func TestRouter_UseGlobals(t *testing.T) {
	templates := []testTemplate{
		{"v1/index.tmpl", "{{.globals.global1}},{{.globals.global2}}"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	req := httptest.NewRequest("GET", "/v1", nil)
	rec := httptest.NewRecorder()

	router.Use(
		Global("global1", func() any {
			return "value1"
		}),
		Global("global2", func() any {
			return "value2"
		}),
	)

	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "value1,value2", string(body))
}

func TestRouter_UseGlobalFunctions(t *testing.T) {
	templates := []testTemplate{
		{"v1/{var1}/index.tmpl", "{{.path_variables.var1 | to_upper}}"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	router.Use(
		Func("to_upper", func(values ...string) string {
			result := ""
			for _, v := range values {
				result += strings.ToUpper(v)
			}
			return result
		}),
	)

	req := httptest.NewRequest("GET", "/v1/val", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "VAL", string(body))
}

func TestRouter_Get(t *testing.T) {
	templates := []testTemplate{
		{"v1/{var1}/v2/{var2}/index.tmpl", "{{.handler_result}},{{.path_variables.var2}}"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	data := url.Values{}
	data.Add("input1", "value1")
	data.Add("d_form_handler", "handler1")
	req := httptest.NewRequest("GET", "/v1/value1/v2/value2?"+data.Encode(), nil)
	rec := httptest.NewRecorder()
	resolverCalled := false

	router.Use(
		FormHandler("handler1", Get(func(w http.ResponseWriter, r *http.Request) (any, error) {
			daveRequest := GetRequest(r.Context())
			pathVariables := daveRequest.PathVariables()
			resolverCalled = true
			value := VariableValue(r, "var1")
			assert.Equal(t, "value1", value)
			assert.Equal(t, "value1", pathVariables["var1"])
			assert.Equal(t, "value2", pathVariables["var2"])
			daveRequest.Template()
			return "resolvedValue", nil
		}),
		),
	)

	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.True(t, resolverCalled, "resolver wasn't called")
	assert.Equal(t, "resolvedValue,value2", string(body))
}

func TestRouter_Post(t *testing.T) {
	templates := []testTemplate{
		{"v1/{var1}/{var2}/index.tmpl", "{{.path_variables.var1}},{{.var1}},{{.var2}}"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	getHandlerCalled := false
	postHandlerCalled := false
	router.Use(
		FormHandler(
			"var1",
			Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
				// daveRequest := GetRequest(r.Context())
				// body, _ := io.ReadAll(r.Body)
				assert.Equal(t, "d_form_handler=var1&input1=value1", r.Form.Encode())
				postHandlerCalled = true
				return "resolved1", nil
			}),
			Get(func(w http.ResponseWriter, r *http.Request) (any, error) {
				getHandlerCalled = true
				return nil, nil
			}),
		),
	)

	data := url.Values{}
	data.Add("input1", "value1")
	data.Add("d_form_handler", "var1")
	req := httptest.NewRequest("POST", "/v1/val/path", strings.NewReader(data.Encode()))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Content-Length", strconv.Itoa(len(data.Encode())))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	assert.True(t, postHandlerCalled, "POST handler wasn't called")
	assert.False(t, getHandlerCalled, "GET handler shouldn't have been called")
}

func TestRouter_Put(t *testing.T) {
	templates := []testTemplate{
		{"v1/{var1}/{var2}/index.tmpl", "{{.path_variables.var1}},{{.handler_result}}"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	handlerCalled := false
	router.Use(
		FormHandler(
			"var1",
			Put(func(w http.ResponseWriter, r *http.Request) (any, error) {
				r.ParseForm()
				w.WriteHeader(202)
				assert.Equal(t, "value1", r.PostForm.Get("input1"))
				handlerCalled = true
				return "resolvedValue", nil
			}),
		),
	)

	data := url.Values{}
	data.Add("input1", "value1")
	data.Add("d_form_handler", "var1")
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

func TestRouter_Patch(t *testing.T) {
	templates := []testTemplate{
		{"v1/{var1}/{var2}/index.tmpl", "{{.path_variables.var1}},{{.handler_result}}"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	handlerCalled := false
	router.Use(
		FormHandler(
			"var1",
			Patch(func(w http.ResponseWriter, r *http.Request) (any, error) {
				r.ParseForm()
				w.WriteHeader(202)
				assert.Equal(t, "value1", r.PostForm.Get("input1"))
				handlerCalled = true
				return "resolvedValue", nil
			}),
		),
	)

	data := url.Values{}
	data.Add("input1", "value1")
	data.Add("d_form_handler", "var1")
	req := httptest.NewRequest("PATCH", "/v1/val/path", strings.NewReader(data.Encode()))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Content-Length", strconv.Itoa(len(data.Encode())))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	resp := rec.Result()

	assert.True(t, handlerCalled, "PATCH handler wasn't called")
	assert.Equal(t, resp.StatusCode, 202)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "val,resolvedValue", string(body))
}

func TestRouter_Delete(t *testing.T) {
	templates := []testTemplate{
		{"v1/{var1}/{var2}/index.tmpl", "{{.path_variables.var1}},{{.handler_result}}"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	handlerCalled := false
	router.Use(
		FormHandler(
			"var1",
			Delete(func(w http.ResponseWriter, r *http.Request) (any, error) {
				w.WriteHeader(202)
				handlerCalled = true
				return "resolvedValue", nil
			}),
		),
	)

	data := url.Values{}
	data.Add("input1", "value1")
	data.Add("d_form_handler", "var1")
	req := httptest.NewRequest("DELETE", "/v1/val/path?"+data.Encode(), nil)
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Content-Length", strconv.Itoa(len(data.Encode())))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	resp := rec.Result()

	assert.True(t, handlerCalled, "DELETE handler wasn't called")
	assert.Equal(t, resp.StatusCode, 202)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "val,resolvedValue", string(body))
}

func TestRouter_UnknownHandler(t *testing.T) {
	templates := []testTemplate{
		{"v1/{var1}/v2/{var2}/index.tmpl", "{{.handler1.GET}},{{.path_variables.var2}}"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	data := url.Values{}
	data.Add("d_form_handler", "handler1")
	req := httptest.NewRequest("GET", "/v1/value1/v2/value2?"+data.Encode(), nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "unexpected error: no registered handler: handler1", string(body))
	assert.Equal(t, 500, resp.StatusCode)
}

func TestRouter_HandlerDoesNotSupportMethod(t *testing.T) {
	templates := []testTemplate{
		{"v1/{var1}/v2/{var2}/index.tmpl", "{{.handler_result}},{{.path_variables.var2}}"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	data := url.Values{}
	data.Add("d_form_handler", "handler1")
	req := httptest.NewRequest("POST", "/v1/value1/v2/value2?"+data.Encode(), nil)
	rec := httptest.NewRecorder()
	resolverCalled := false

	router.Use(
		FormHandler("handler1", Get(func(w http.ResponseWriter, r *http.Request) (any, error) {
			resolverCalled = true
			return "resolvedValue", nil
		}),
		),
	)

	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.False(t, resolverCalled, "resolver was called")
	assert.Equal(t, "unexpected error: handler handler1 does not support method: POST", string(body))
}

func TestRouter_TemplateNotFoundError(t *testing.T) {
	templates := []testTemplate{}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	req := httptest.NewRequest("GET", "/path/to/nothing", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)
	resp := rec.Result()

	assert.Equal(t, resp.StatusCode, http.StatusNotFound)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "not found: no template at /path/to/nothing/index", string(body))
}

func TestRouter_ResourceNotFoundError(t *testing.T) {
	templates := []testTemplate{
		{"v1/{var1}/v2/{var2}/index.tmpl", ""},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	data := url.Values{}
	data.Add("d_form_handler", "var1")
	req := httptest.NewRequest("GET", "/v1/value1/v2/value2?"+data.Encode(), nil)
	req.Header.Add("D-FORM-HANDLER", "var1")
	rec := httptest.NewRecorder()

	router.Use(
		FormHandler(
			"var1",
			Get(func(w http.ResponseWriter, r *http.Request) (any, error) {
				value := VariableValue(r, "var1")
				return nil, NotFound(fmt.Errorf("no entity found for %s", value))
			}),
		),
	)

	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, resp.StatusCode, http.StatusOK, "expected status OK because resolver didn't set response code")
	assert.Equal(t, "not found: no entity found for value1", string(body))
}

func TestRouter_UnexpectedError(t *testing.T) {
	templates := []testTemplate{
		{"v1/{var1}/v2/{var2}/index.tmpl", ""},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	data := url.Values{}
	data.Add("d_form_handler", "var1")
	req := httptest.NewRequest("GET", "/v1/value1/v2/value2?"+data.Encode(), nil)
	rec := httptest.NewRecorder()

	router.Use(
		FormHandler(
			"var1",
			Get(func(w http.ResponseWriter, r *http.Request) (any, error) {
				value := VariableValue(r, "var1")
				return nil, Unexpected(fmt.Errorf("some unexpected error resolving var1=%s", value))
			}),
		),
	)

	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "text/plain; charset=utf-8", resp.Header.Get("Content-Type"))
	assert.Equal(t, "unexpected error: some unexpected error resolving var1=value1", string(body))
}

func TestRouter_UnexpectedErrorFallback(t *testing.T) {
	templates := []testTemplate{
		{"v1/{var1}/v2/{var2}/index.tmpl", ""},
		{"fallback/unexpected_error.tmpl", "500, unexpected_error: {{.error}}"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	data := url.Values{}
	data.Add("d_form_handler", "var1")
	req := httptest.NewRequest("GET", "/v1/value1/v2/value2?"+data.Encode(), nil)
	rec := httptest.NewRecorder()

	router.Use(
		FormHandler(
			"var1",
			Get(func(w http.ResponseWriter, r *http.Request) (any, error) {
				value := VariableValue(r, "var1")
				return nil, Unexpected(fmt.Errorf("some unexpected error resolving var1=%s", value))
			}),
		),
	)

	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "500, unexpected_error: some unexpected error resolving var1=value1", string(body))
}

func TestRouter_ResourceNotFoundErrorFallback(t *testing.T) {
	templates := []testTemplate{
		{"v1/{var1}/v2/{var2}/index.tmpl", ""},
		{"fallback/not_found.tmpl", "404, not found: {{.error}}"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	data := url.Values{}
	data.Add("d_form_handler", "var1")
	req := httptest.NewRequest("GET", "/v1/value1/v2/value2?"+data.Encode(), nil)
	rec := httptest.NewRecorder()

	router.Use(
		FormHandler(
			"var1",
			Get(func(w http.ResponseWriter, r *http.Request) (any, error) {
				value := VariableValue(r, "var1")
				return nil, NotFound(fmt.Errorf("no entity found for %s", value))
			}),
		),
	)

	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "text/html; charset=utf-8", resp.Header.Get("Content-Type"))
	assert.Equal(t, resp.StatusCode, http.StatusOK, "expected status OK because resolver didn't set response code")
	assert.Equal(t, "404, not found: no entity found for value1", string(body))
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

func TestRouter_LayoutResolver_ReturnsLayout(t *testing.T) {
	templates := []testTemplate{
		{"layouts/custom.tmpl", "custom-start {{.content}} custom-end"},
		{"path/to/index.tmpl", "page-content"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	resolverCalled := false
	router.Use(
		LayoutResolver(func(r *http.Request) string {
			resolverCalled = true
			return "custom"
		}),
	)

	req := httptest.NewRequest("GET", "/path/to", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)

	assert.True(t, resolverCalled, "layout resolver should have been called")
	assert.Equal(t, "custom-start page-content custom-end", string(body))
}

func TestRouter_LayoutResolver_EmptyStringSkipsLayout(t *testing.T) {
	templates := []testTemplate{
		{"layouts/default.tmpl", "default-start {{.content}} default-end"},
		{"path/to/index.tmpl", "page-content"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	router.Use(
		LayoutResolver(func(r *http.Request) string {
			return "" // empty string should skip layout
		}),
	)

	req := httptest.NewRequest("GET", "/path/to", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)

	// Should render without any layout wrapping
	assert.Equal(t, "page-content", string(body))
}

func TestRouter_LayoutResolver_HXRequestSkipsLayout(t *testing.T) {
	templates := []testTemplate{
		{"layouts/default.tmpl", "default-start {{.content}} default-end"},
		{"path/to/index.tmpl", "partial-content"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	router.Use(
		LayoutResolver(func(r *http.Request) string {
			if r.Header.Get("HX-Request") == "true" {
				return ""
			}
			return "default"
		}),
	)

	req := httptest.NewRequest("GET", "/path/to", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, "partial-content", string(body))

	req2 := httptest.NewRequest("GET", "/path/to", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	resp2 := rec2.Result()
	body2, _ := io.ReadAll(resp2.Body)
	assert.Equal(t, "default-start partial-content default-end", string(body2))
}

func TestRouter_LayoutResolver_DLayoutHeaderOverridesResolver(t *testing.T) {
	templates := []testTemplate{
		{"layouts/resolver-layout.tmpl", "resolver-start {{.content}} resolver-end"},
		{"layouts/header-layout.tmpl", "header-start {{.content}} header-end"},
		{"path/to/index.tmpl", "page-content"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	router.Use(
		LayoutResolver(func(r *http.Request) string {
			return "resolver-layout"
		}),
	)

	req := httptest.NewRequest("GET", "/path/to", nil)
	req.Header.Set("D-LAYOUT", "header-layout")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)

	assert.Equal(t, "header-start page-content header-end", string(body))
}

func TestRouter_LayoutResolver_ResolverReceivesRequest(t *testing.T) {
	templates := []testTemplate{
		{"layouts/default.tmpl", "{{.content}}"},
		{"path/to/index.tmpl", "content"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	var capturedPath string
	var capturedMethod string
	var capturedHeader string

	router.Use(
		LayoutResolver(func(r *http.Request) string {
			capturedPath = r.URL.Path
			capturedMethod = r.Method
			capturedHeader = r.Header.Get("X-Custom-Header")
			return "default"
		}),
	)

	req := httptest.NewRequest("POST", "/path/to", nil)
	req.Header.Set("X-Custom-Header", "custom-value")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, "/path/to", capturedPath)
	assert.Equal(t, "POST", capturedMethod)
	assert.Equal(t, "custom-value", capturedHeader)
}

func TestRouter_LayoutResolver_NonExistentLayoutFallsBackToNoLayout(t *testing.T) {
	templates := []testTemplate{
		{"path/to/index.tmpl", "page-content"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	router.Use(
		LayoutResolver(func(r *http.Request) string {
			return "non-existent-layout"
		}),
	)

	req := httptest.NewRequest("GET", "/path/to", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)

	assert.Equal(t, "page-content", string(body))
}

func TestRouter_ExplicitPathTakesPrecedenceOverPathVariable(t *testing.T) {
	templates := []testTemplate{
		{"users/{id}/index.tmpl", "user-id:{{.path_variables.id}}"},
		{"users/new/index.tmpl", "new-user-form"},
		{"users/{id}/posts/{postId}/index.tmpl", "user:{{.path_variables.id}},post:{{.path_variables.postId}}"},
		{"users/{id}/posts/latest/index.tmpl", "user:{{.path_variables.id}},latest-post"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()
	testCases := []struct {
		name           string
		path           string
		expectedRender string
	}{
		{
			name:           "explicit 'new' should not be captured as path variable",
			path:           "/users/new",
			expectedRender: "new-user-form",
		},
		{
			name:           "other values should still use path variable route",
			path:           "/users/123",
			expectedRender: "user-id:123",
		},
		{
			name:           "nested explicit 'latest' should not be captured as path variable",
			path:           "/users/456/posts/latest",
			expectedRender: "user:456,latest-post",
		},
		{
			name:           "nested other values should still use path variable route",
			path:           "/users/456/posts/789",
			expectedRender: "user:456,post:789",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			resp := rec.Result()
			body, _ := io.ReadAll(resp.Body)
			assert.Equal(t, tc.expectedRender, string(body))
		})
	}
}

func TestRouter_LogsErrorWhenHandlerWritesToResponseBody(t *testing.T) {
	templates := []testTemplate{
		{"path/to/index.tmpl", "template-content"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	var logBuffer bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
	slog.SetDefault(logger)

	router.Use(
		FormHandler("writeBody",
			Get(func(w http.ResponseWriter, r *http.Request) (any, error) {
				w.Write([]byte("direct write"))
				return "handler-result", nil
			}),
		),
	)

	data := url.Values{}
	data.Add("d_form_handler", "writeBody")
	req := httptest.NewRequest("GET", "/path/to?"+data.Encode(), nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	logOutput := logBuffer.String()
	assert.Contains(t, logOutput, "handler wrote to response body")
}

func TestRouter_DX_RescanTemplates(t *testing.T) {
	templates := []testTemplate{
		{"path/index.tmpl", ""},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	router.Use(
		Config(&Conf{
			DevMode: true,
		}),
	)

	req1 := httptest.NewRequest("GET", "/v1/value1/v2/value2", nil)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	originalTemplates := router.templates

	req2 := httptest.NewRequest("GET", "/v1/value1/v2/value2", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)

	assert.NotEqual(t, originalTemplates, router.templates)
}

// func TestRouter_DX_CacheTemplateData(t *testing.T) {
// 	templates := []testTemplate{
// 		{"v1/{var1}/v2/{var2}/index.tmpl", "{{.handler_result}},{{.path_variables.var2}}"},
// 	}
// 	router, cleanup := prepareTest(templates)
// 	defer cleanup()
//
// 	data := url.Values{}
// 	data.Add("input1", "value1")
// 	data.Add("d_form_handler", "handler1")
// 	req := httptest.NewRequest("GET", "/v1/value1/v2/value2?"+data.Encode(), nil)
// 	rec := httptest.NewRecorder()
// 	resolverCalled := false
//
// 	router.Use(
// 		Config(&Conf{
// 			DevMode: true,
// 		}),
// 		FormHandler("handler1", Get(func(w http.ResponseWriter, r *http.Request) (any, error) {
// 			resolverCalled = true
// 			return "resolvedValue", nil
// 		}),
// 		),
// 	)
// 	router.ServeHTTP(rec, req)
// 	assert.True(t, resolverCalled)
//
// 	resolverCalled = false
// 	req2 := httptest.NewRequest("GET", "/v1/value1/v2/value2?"+data.Encode(), nil)
// 	req2.Header.Add("D-DEV", "true")
// 	rec2 := httptest.NewRecorder()
// 	router.ServeHTTP(rec2, req2)
//
// 	assert.False(t, resolverCalled)
// }

func TestRouter_ConfigurableDefaultLayout(t *testing.T) {
	templates := []testTemplate{
		{"layouts/main.tmpl", "main-layout:{{.content}}"},
		{"layouts/default.tmpl", "default-layout:{{.content}}"},
		{"path/to/index.tmpl", "page-content"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	router.Use(
		Config(&Conf{
			DefaultLayout: "main",
		}),
	)

	req := httptest.NewRequest("GET", "/path/to", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)

	assert.Equal(t, "main-layout:page-content", string(body))
}

func TestRouter_ConfigurableTemplateExtension(t *testing.T) {
	templates := []testTemplate{
		{"path/to/index.html", "html-template"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	router.Use(
		Config(&Conf{
			TemplateExtension: ".html",
		}),
	)

	req := httptest.NewRequest("GET", "/path/to", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)

	assert.Equal(t, "html-template", string(body))
}

func TestRouter_ConfigurableTemplateExtensionWithLayout(t *testing.T) {
	templates := []testTemplate{
		{"layouts/default.html", "layout:{{.content}}"},
		{"path/to/index.html", "page-content"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	router.Use(
		Config(&Conf{
			TemplateExtension: ".html",
		}),
	)

	req := httptest.NewRequest("GET", "/path/to", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)

	assert.Equal(t, "layout:page-content", string(body))
}

func TestRouter_ScanTemplates_InvalidTemplate_ReturnsErrorInResponse(t *testing.T) {
	templates := []testTemplate{
		{"path/to/index.tmpl", "{{.unclosed"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	req := httptest.NewRequest("GET", "/path/to", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	resp := rec.Result()
	body, _ := io.ReadAll(resp.Body)

	assert.Contains(t, string(body), "error")
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestRouter_MultipartForm_ConfigurableMaxFormSize(t *testing.T) {
	templates := []testTemplate{
		{"path/to/index.tmpl", "result:{{.handler_result}}"},
	}
	router, cleanup := prepareTest(templates)
	defer cleanup()

	router.Use(
		Config(&Conf{
			MaxFormSize: 1 << 20, // 1MB
		}),
		FormHandler("myHandler",
			Post(func(w http.ResponseWriter, r *http.Request) (any, error) {
				return r.FormValue("text_field"), nil
			}),
		),
	)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	writer.WriteField("d_form_handler", "myHandler")
	writer.WriteField("text_field", "configured-size")
	writer.Close()

	req := httptest.NewRequest("POST", "/path/to", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	resp := rec.Result()
	respBody, _ := io.ReadAll(resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "result:configured-size", string(respBody))
}
