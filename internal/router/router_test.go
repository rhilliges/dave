package router

import (
	"fmt"
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

// FEATURES:
// - file based routing - done
// - path variables - done
// - resolvers - done
// - CRUD - done
// - - POST - done
// - - PUT - done
// - - PATCH - done
// - - DELETE - done
// - components (nested templates) - done
// - TEMPLATE_NAME header - done
// - layouts
// - - default layout - done
// - - LAYOUT header - done
// - - layout resolvers (HX-Request header example, D-LAYOUT default implementation) - TODO
// - globals
// - - global available values - done
// - - global template functions - done
// - error handling
// - - logging (log unexpected errors if some rendering failed) - TODO
// - - validation error during POST/PATCH/PUT - done (use HX-Location header)
// - - redirect error - done (use HX-Location header)
// - - fallback templates (unexpected error, not found) - done
// - content response headers (html, text) - done
// - d_form_handler header - done
// - cache data to render template for quick browser refreshes - done
// - user writes to ResponseWriter -> panic and tell the user why not to do that - TODO
// - make header case insensitive (double check if needed)
// - make configurable
// - - default layout
// - - default file extension
//
// DOCUMENTATION:
// - requets lifecycle
// - - template priority: index -> D-TEMPLATE -> HandlerMethod.template
// - - layout priority: default -> layout resolvers -> HandlerMethod.layout
// - only write error response codes, otherwise could hide error response codes from previous handlers
// - globals
// - - i18n example implementation
// - available headers
// - HandlerMethod:
// - - router.HandlerMethod is available for full control but should better not be used (use HX-Location)
// - document router scanTemplates function (make public?)

// What to do next:
// - logging (log unexpected errors if some rendering failed) -> add/remove "DAVE" context variable
// - dev experience (caching, scanTemplates)
// - clone root templates before rendering
// - custom fallback templates for e.g. auth errors
// - SKIP_RESOLVER header (make configurable)
// - SKIP_GLOBAL_VALUES header (make configurable)
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
