package router

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

type (
	FormHandlerConfFunc func(router *Router, varName string)
	FormHandlerFunc     func(w http.ResponseWriter, r *http.Request) (any, error)
	LayoutResolverFunc  func(r *http.Request) string
	ConfFunc            func(router *Router)
)

type Router struct {
	fs             fs.FS
	formHandlers   map[string]map[string]FormHandlerFunc
	globals        map[string]func() any
	templates      *template.Template
	funcs          map[string]any
	config         *Conf
	lastRender     *Render
	layoutResolver LayoutResolverFunc
}

type Render struct {
	template       string
	pathVariables  map[string]string
	globals        map[string]any
	resolvedValues map[string]any
	layout         string
}

func (r *Render) Template() string {
	return r.template
}

func (r *Render) PathVariables() map[string]string {
	return r.pathVariables
}

func (r *Render) Layout() string {
	return r.layout
}

func (r *Render) ResolvedValues() map[string]any {
	return r.resolvedValues
}

func (r *Render) Globals() map[string]any {
	return r.globals
}

const RequestContextKey = "dave-request"

type loggerContextKey struct{}

func LoggerFromContext(ctx context.Context) *slog.Logger {
	if logger, ok := ctx.Value(loggerContextKey{}).(*slog.Logger); ok {
		return logger
	}
	return slog.Default()
}

func contextWithLogger(ctx context.Context, logger *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerContextKey{}, logger)
}

func (router *Router) Use(configFunc ...ConfFunc) {
	for _, f := range configFunc {
		f(router)
	}
}

func Config(c *Conf) ConfFunc {
	return func(router *Router) {
		router.config = c
	}
}

type Conf struct {
	DevMode bool
}

func Func(s string, f any) ConfFunc {
	return func(router *Router) {
		router.funcs[s] = f
	}
}

func Global(name string, globalFunc func() any) ConfFunc {
	return func(router *Router) {
		router.globals[name] = globalFunc
	}
}

func LayoutResolver(resolver LayoutResolverFunc) ConfFunc {
	return func(router *Router) {
		router.layoutResolver = resolver
	}
}

func FormHandler(s string, handlerFunc ...FormHandlerConfFunc) ConfFunc {
	return func(router *Router) {
		for _, f := range handlerFunc {
			f(router, s)
		}
	}
}

func Get(handler FormHandlerFunc) FormHandlerConfFunc {
	return MethodHandler(http.MethodGet, handler)
}

func Post(resolverFunc FormHandlerFunc) FormHandlerConfFunc {
	return MethodHandler(http.MethodPost, resolverFunc)
}

func Put(resoverFunc FormHandlerFunc) FormHandlerConfFunc {
	return MethodHandler(http.MethodPut, resoverFunc)
}

func Patch(resolverFunc FormHandlerFunc) FormHandlerConfFunc {
	return MethodHandler(http.MethodPatch, resolverFunc)
}

func Delete(resoverFunc FormHandlerFunc) FormHandlerConfFunc {
	return MethodHandler(http.MethodDelete, resoverFunc)
}

func MethodHandler(m string, handler FormHandlerFunc) FormHandlerConfFunc {
	return func(router *Router, varName string) {
		variableResolvers := router.formHandlers[varName]
		if variableResolvers == nil {
			router.formHandlers[varName] = make(map[string]FormHandlerFunc)
		}
		router.formHandlers[varName][m] = func(w http.ResponseWriter, r *http.Request) (any, error) {
			return handler(w, r)
		}
	}
}

func NewRouter(fs fs.FS) *Router {
	return &Router{
		fs:           fs,
		formHandlers: make(map[string]map[string]FormHandlerFunc),
		globals:      make(map[string]func() any),
		funcs:        make(map[string]any),
		config:       &Conf{},
	}
}

func (router *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestLogger := slog.Default().With("request_id", uuid.New(), "method", r.Method, "path", r.URL.Path)
	requestLogger.Info("request started")
	defer requestLogger.Info("request completed", "duration_ms", time.Since(start).Milliseconds())

	r = r.WithContext(contextWithLogger(r.Context(), requestLogger))

	if router.templates == nil || router.config.DevMode {
		router.ScanTemplates()
	}
	render, err := router.getRender(w, r)
	rootTemplate, _ := router.templates.Clone()
	data := make(map[string]any)
	if err != nil {
		daveError := &daveError{}
		if errors.As(err, daveError) {
			requestLogger.Info("returning error response", "error_type", daveError.message, "cause", daveError.cause)
			t := rootTemplate.Lookup(daveError.fallback)
			if t != nil {
				data["error"] = daveError.cause
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				err = rootTemplate.ExecuteTemplate(w, daveError.fallback, data)
				if err != nil {
					requestLogger.Error("error rendering fallback template", "template", daveError.fallback)
				}
			} else {
				requestLogger.Error("cannot find fallback template", "template", daveError.fallback)
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.Write([]byte(fmt.Sprintf("%s: %s", daveError.message, daveError.cause)))
			}
		} else {
			requestLogger.Error("unexpected error during request processing", "error", err)
		}
		return
	}
	data = render.resolvedValues
	data["globals"] = render.globals
	data["path_variables"] = render.pathVariables
	if render.layout == "" {
		requestLogger.Debug("rendering without layout", "template", render.template)
		err = rootTemplate.ExecuteTemplate(w, render.template, data)
		if err != nil {
			requestLogger.Error("template execution failed", "template", render.template, "error", err)
		}
		return
	}
	content := &strings.Builder{}
	requestLogger.Debug("rendering with layout", "template", render.template, "layout", render.layout)
	err = rootTemplate.ExecuteTemplate(content, render.template, data)
	if err != nil {
		requestLogger.Error("template execution failed", "template", render.template, "error", err)
		router.handleTemplateError(w, r)
		return
	}
	err = rootTemplate.ExecuteTemplate(w, render.layout, map[string]string{"content": content.String()})
	if err != nil {
		requestLogger.Error("layout execution failed", "layout", render.layout, "error", err)
		router.handleTemplateError(w, r)
		return
	}
}

func (router *Router) handleTemplateError(w http.ResponseWriter, r *http.Request) {
	logger := LoggerFromContext(r.Context())
	logger.Error("template error occurred")
	w.Write([]byte("error executing template"))
}

func (router *Router) ScanTemplates() {
	slog.Info("scanning templates")
	rootTemplate := template.New(time.Now().String())
	rootTemplate.Funcs(router.funcs)
	root := router.fs
	fs.WalkDir(root, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Error("failed to walk directory", "path", path, "error", err)
			panic(err)
		}
		if path == "." {
			return nil
		}
		if d.IsDir() {
			slog.Debug("found directory", "dir", path)
			return nil
		}
		slog.Debug("parsing template", "template", path)
		newTemplate := rootTemplate.New(stripTemplateSuffix(path))
		file, err := root.Open(path)
		if err != nil {
			slog.Error("failed to open template file", "template", path, "error", err)
			panic(err)
		}
		defer file.Close()
		content, err := io.ReadAll(file)
		if err != nil {
			slog.Error("failed to read template file", "template", path, "error", err)
			panic(err)
		}
		_, err = newTemplate.Parse(string(content))
		if err != nil {
			slog.Error("failed to parse template", "template", path, "error", err)
			panic(err)
		}
		return nil
	})
	slog.Info("template scanning complete", "count", len(rootTemplate.Templates()))
	router.templates = rootTemplate
}

func (router *Router) getRender(w http.ResponseWriter, r *http.Request) (*Render, error) {
	logger := LoggerFromContext(r.Context())
	logger.Debug("creating render object")
	globals := make(map[string]any)
	resolvedValues := make(map[string]any)
	pathVariables := make(map[string]string)
	template := ""
	layout := ""

	templateName := r.Header.Get("D-TEMPLATE")
	if templateName == "" {
		templateName = "index"
	}

	layout = r.Header.Get("D-LAYOUT")
	if layout == "" && router.layoutResolver != nil {
		layout = router.layoutResolver(r)
		if layout == "" {
			logger.Debug("layout resolver returned empty string; rendering w/o layout")
		}
	} else if layout == "" {
		layout = "default"
	}

	if layout != "" {
		layout = strings.Join([]string{"layouts", layout}, "/")
		layoutTemplate := router.templates.Lookup(layout)
		if layoutTemplate == nil {
			logger.Debug("layout not found; rendering w/o layout", "layout", layout)
			layout = ""
		}
	}

	reqPath := strings.Join([]string{r.URL.Path, templateName}, "/")
	template, pathVariables = parseRequestPath(router.templates, reqPath)
	logger.Debug("resolved template", "template", template, "path_variables", pathVariables)

	for name, global := range router.globals {
		globals[name] = global()
	}

	if template == "" {
		logger.Debug("template not found", "requested_path", reqPath)
		w.WriteHeader(http.StatusNotFound)
		return &Render{
				template,
				pathVariables,
				globals,
				resolvedValues,
				layout,
			},
			NotFound(fmt.Errorf("no template at %s", reqPath))
	}

	keys := make([]string, 0, len(pathVariables))
	for k := range pathVariables {
		keys = append(keys, k)
	}

	if err := r.ParseForm(); err != nil {
		logger.Error("failed to parse form", "error", err)
	}
	formHandlerKey := r.FormValue("d_form_handler")
	if formHandlerKey != "" {
		logger.Info("executing form handler", "handler", formHandlerKey, "method", r.Method)
		handler := router.formHandlers[formHandlerKey]
		if handler == nil {
			logger.Error("no registered handler", "handler", formHandlerKey)
			w.WriteHeader(500)
			return nil, Unexpected(fmt.Errorf("no registered handler: %s", formHandlerKey))
		}
		handlerMethod := handler[r.Method]
		if handlerMethod == nil {
			logger.Error("handler does not support method", "handler", formHandlerKey, "method", r.Method)
			w.WriteHeader(500)
			return nil, Unexpected(fmt.Errorf("handler %s does not support method: %s", formHandlerKey, r.Method))
		}
		resolverCtx := context.WithValue(
			r.Context(),
			RequestContextKey,
			Render{
				template,
				pathVariables,
				globals,
				resolvedValues,
				layout,
			})
		resolverReq := r.WithContext(resolverCtx)
		val, err := handlerMethod(w, resolverReq)
		if err != nil {
			logger.Info("form handler returned error", "handler", formHandlerKey, "error", err)
			return nil, err
		}
		logger.Info("form handler completed", "handler", formHandlerKey)
		resolvedValues["handler_result"] = val
	}

	return &Render{
		template,
		pathVariables,
		globals,
		resolvedValues,
		layout,
	}, nil
}

func parseRequestPath(templates *template.Template, path string) (string, map[string]string) {
	reqSegments := strings.Split(path[1:], "/")
	templatePath := ""
	pathVariables := make(map[string]string)

	for _, v := range templates.Templates() {
		// if v.Name() == "root" {
		// 	continue
		// }
		path := stripTemplateSuffix(v.Name())
		pathSegments := strings.Split(path, "/")
		if len(pathSegments) != len(reqSegments) {
			continue
		}
		found := true
		for i, seg := range pathSegments {
			if seg == reqSegments[i] {
				continue
			} else {
				if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
					varName := seg[1 : len(seg)-1]
					pathVariables[varName] = reqSegments[i]
				} else {
					found = false
					break
				}
			}
		}
		if found {
			templatePath = path
		} else {
			pathVariables = make(map[string]string)
		}
	}
	return templatePath, pathVariables
}

func stripTemplateSuffix(t string) string {
	i := strings.LastIndex(t, ".tmpl")
	if i < 0 {
		return t
	}
	return t[:i]
}

func GetRequest(context context.Context) Render {
	return context.Value(RequestContextKey).(Render)
}

func VariableValue(r *http.Request, varName string) any {
	request := r.Context().Value(RequestContextKey).(Render)
	return request.pathVariables[varName]
}

type daveError struct {
	message  string
	fallback string
	cause    error
}

func (daveError daveError) Error() string {
	return daveError.message
}

func NotFound(cause error) error {
	return daveError{
		message:  "not found",
		fallback: "fallback/not_found",
		cause:    cause,
	}
}

func Unexpected(cause error) error {
	return daveError{
		message:  "unexpected error",
		fallback: "fallback/unexpected_error",
		cause:    cause,
	}
}
