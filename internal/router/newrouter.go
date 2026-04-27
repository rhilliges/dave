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
	DevMode           bool
	DefaultLayout     string
	TemplateExtension string
	MaxFormSize       int64
}

func (c *Conf) getDefaultLayout() string {
	if c.DefaultLayout == "" {
		return "default"
	}
	return c.DefaultLayout
}

func (c *Conf) getTemplateExtension() string {
	if c.TemplateExtension == "" {
		return ".tmpl"
	}
	return c.TemplateExtension
}

func (c *Conf) getMaxFormSize() int64 {
	if c.MaxFormSize == 0 {
		return 32 << 20 // 32MB default
	}
	return c.MaxFormSize
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
		if err := router.ScanTemplates(); err != nil {
			requestLogger.Error("failed to scan templates", "error", err)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(fmt.Sprintf("error scanning templates: %s", err)))
			return
		}
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

func (router *Router) ScanTemplates() error {
	slog.Info("scanning templates")
	rootTemplate := template.New(time.Now().String())
	rootTemplate.Funcs(router.funcs)
	ext := router.config.getTemplateExtension()
	root := router.fs
	var scanErr error
	fs.WalkDir(root, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			slog.Error("failed to walk directory", "path", path, "error", err)
			scanErr = fmt.Errorf("failed to walk directory %s: %w", path, err)
			return scanErr
		}
		if path == "." {
			return nil
		}
		if d.IsDir() {
			slog.Debug("found directory", "dir", path)
			return nil
		}
		if !strings.HasSuffix(path, ext) {
			slog.Debug("skipping non-template file", "file", path)
			return nil
		}
		slog.Debug("parsing template", "template", path)
		newTemplate := rootTemplate.New(stripTemplateSuffix(path, ext))
		file, err := root.Open(path)
		if err != nil {
			slog.Error("failed to open template file", "template", path, "error", err)
			scanErr = fmt.Errorf("failed to open template file %s: %w", path, err)
			return scanErr
		}
		defer file.Close()
		content, err := io.ReadAll(file)
		if err != nil {
			slog.Error("failed to read template file", "template", path, "error", err)
			scanErr = fmt.Errorf("failed to read template file %s: %w", path, err)
			return scanErr
		}
		_, err = newTemplate.Parse(string(content))
		if err != nil {
			slog.Error("failed to parse template", "template", path, "error", err)
			scanErr = fmt.Errorf("failed to parse template %s: %w", path, err)
			return scanErr
		}
		return nil
	})
	if scanErr != nil {
		return scanErr
	}
	slog.Info("template scanning complete", "count", len(rootTemplate.Templates()))
	router.templates = rootTemplate
	return nil
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
		layout = router.config.getDefaultLayout()
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
	template, pathVariables = router.parseRequestPath(router.templates, reqPath)
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

	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(router.config.getMaxFormSize()); err != nil {
			logger.Error("failed to parse multipart form", "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return nil, Unexpected(fmt.Errorf("failed to parse multipart form: %w", err))
		}
	} else {
		if err := r.ParseForm(); err != nil {
			logger.Error("failed to parse form", "error", err)
			w.WriteHeader(http.StatusBadRequest)
			return nil, Unexpected(fmt.Errorf("failed to parse form: %w", err))
		}
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
		guardedWriter := &guardedResponseWriter{ResponseWriter: w, logger: logger}
		val, err := handlerMethod(guardedWriter, resolverReq)
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

func (router *Router) parseRequestPath(templates *template.Template, path string) (string, map[string]string) {
	reqSegments := strings.Split(path[1:], "/")
	templatePath := ""
	pathVariables := make(map[string]string)
	bestSpecificity := -1
	ext := router.config.getTemplateExtension()

	for _, v := range templates.Templates() {
		path := stripTemplateSuffix(v.Name(), ext)
		pathSegments := strings.Split(path, "/")
		if len(pathSegments) != len(reqSegments) {
			continue
		}
		found := true
		specificity := 0
		candidateVars := make(map[string]string)
		for i, seg := range pathSegments {
			if seg == reqSegments[i] {
				specificity++
				continue
			} else {
				if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
					varName := seg[1 : len(seg)-1]
					candidateVars[varName] = reqSegments[i]
				} else {
					found = false
					break
				}
			}
		}
		if found && specificity > bestSpecificity {
			bestSpecificity = specificity
			templatePath = path
			pathVariables = candidateVars
		}
	}
	return templatePath, pathVariables
}

func stripTemplateSuffix(t string, ext string) string {
	i := strings.LastIndex(t, ext)
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

type guardedResponseWriter struct {
	http.ResponseWriter
	logger    *slog.Logger
	bodyWrote bool
}

func (g *guardedResponseWriter) Write(b []byte) (int, error) {
	if !g.bodyWrote {
		g.logger.Error("handler wrote to response body", "bytes", len(b))
		g.bodyWrote = true
	}
	// Ignore the write but return success to avoid errors in handler
	return len(b), nil
}
