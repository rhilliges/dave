package dave

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

func (handlerFunc FormHandlerFunc) call(w http.ResponseWriter, r *http.Request, render *Render) (any, error) {
	resolverCtx := context.WithValue(r.Context(), requestContextKey{}, *render)
	resolverReq := r.WithContext(resolverCtx)
	guardedWriter := &guardedResponseWriter{ResponseWriter: w}
	val, err := handlerFunc(guardedWriter, resolverReq)
	if err != nil {
		slog.Info("form handler returned error", "error", err)
		return nil, err
	}
	slog.Info("form handler completed")
	return val, nil
}

type errorTypeMapping struct {
	target   error
	status   int
	fallback string
}

type Router struct {
	fs             fs.FS
	formHandlers   map[string]map[string]FormHandlerFunc
	globals        map[string]func(render *Render) any
	templateFuncs  map[string]func(*Render) any
	templates      *template.Template
	config         *Conf
	lastRender     *Render
	layoutResolver LayoutResolverFunc
	errorTypes     []errorTypeMapping
}

type Render struct {
	request       *http.Request
	template      string
	pathVariables map[string]string
	globals       map[string]any
	handlerResult any
	layout        string
}

func (r *Render) Request() *http.Request {
	return r.request
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

func (r *Render) Globals() map[string]any {
	return r.globals
}

func (r *Render) HandlerResult() any {
	return r.handlerResult
}

func (r *Render) FormResponse() *FormResponse {
	if formResponse, ok := r.handlerResult.(*FormResponse); ok {
		return formResponse
	}
	return nil
}

type requestContextKey struct{}

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

func Func(name string, factory func(*Render) any) ConfFunc {
	return func(router *Router) {
		router.templateFuncs[name] = factory
	}
}

func Global(name string, globalFunc func(render *Render) any) ConfFunc {
	return func(router *Router) {
		router.globals[name] = globalFunc
	}
}

func LayoutResolver(resolver LayoutResolverFunc) ConfFunc {
	return func(router *Router) {
		router.layoutResolver = resolver
	}
}

func ErrorType(target error, status int, fallbackName string) ConfFunc {
	return func(router *Router) {
		router.errorTypes = append(router.errorTypes, errorTypeMapping{
			target:   target,
			status:   status,
			fallback: "fallback/" + fallbackName,
		})
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

func Put(resolverFunc FormHandlerFunc) FormHandlerConfFunc {
	return MethodHandler(http.MethodPut, resolverFunc)
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
		fs:            fs,
		formHandlers:  make(map[string]map[string]FormHandlerFunc),
		globals:       make(map[string]func(render *Render) any),
		templateFuncs: make(map[string]func(*Render) any),
		config:        &Conf{},
	}
}

func (router *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	logger := slog.Default().With("request_id", uuid.New(), "method", r.Method, "path", r.URL.Path)
	logger.Info("request started")
	defer logger.Info("request completed", "duration_ms", time.Since(start).Milliseconds())

	r = r.WithContext(contextWithLogger(r.Context(), logger))

	if router.templates == nil || router.config.DevMode {
		if err := router.ScanTemplates(); err != nil {
			logger.Error("failed to scan templates", "error", err)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(fmt.Sprintf("error scanning templates: %s", err)))
			return
		}
	}
	render := &Render{
		request:       r,
		pathVariables: make(map[string]string),
		globals:       make(map[string]any),
		handlerResult: &FormResponse{},
	}
	rootTemplate, _ := router.templates.Clone() // TODO handle error
	renderFuncs := make(template.FuncMap)
	for name, factory := range router.templateFuncs {
		renderFuncs[name] = factory(render)
	}
	rootTemplate.Funcs(renderFuncs)

	render.layout = router.getLayout(r)
	templateName, pathVariables, daveErr := router.parseRequestPath(r)
	if daveErr != nil {
		logger.Debug("template not found")
		router.renderError(w, rootTemplate, daveErr)
		return
	}
	render.template = templateName
	render.pathVariables = pathVariables

	logger.Debug("resolved template", "template", render.template, "path_variables", render.pathVariables)

	for name, global := range router.globals {
		render.globals[name] = global(render)
	}

	handler, daveErr := router.getFormHandler(r)
	if daveErr != nil {
		router.renderError(w, rootTemplate, daveErr)
		return
	}

	if handler != nil {
		handlerResult, err := handler.call(w, r, render)
		if err != nil {
			router.renderError(w, rootTemplate, err)
			return
		}
		render.handlerResult = handlerResult
	}

	data := make(map[string]any)
	if formResponse, ok := render.handlerResult.(*FormResponse); ok {
		data["form"] = formResponse
		data["result"] = formResponse.Result
	} else {
		data["result"] = render.handlerResult
	}
	data["globals"] = render.globals
	data["path_variables"] = render.pathVariables

	contentWriter := &strings.Builder{}
	logger.Debug("rendering template", "template", render.template, "layout", render.layout)
	err := rootTemplate.ExecuteTemplate(contentWriter, render.template, data)
	if err != nil {
		logger.Error("template execution failed", "template", render.template, "error", err)
		router.renderError(w, rootTemplate, err)
		return
	}

	if render.layout == "" {
		w.Write([]byte(contentWriter.String())) // TODO handle error
		return
	}

	layoutData := map[string]any{
		"content": template.HTML(contentWriter.String()),
		"globals": render.globals,
	}
	pageWriter := &strings.Builder{}
	err = rootTemplate.ExecuteTemplate(pageWriter, render.layout, layoutData)
	if err != nil {
		logger.Error("layout execution failed", "layout", render.layout, "error", err)
		router.renderError(w, rootTemplate, err)
		return
	}
	w.Write([]byte(pageWriter.String())) // TODO handle error
}

func (router *Router) getLayout(r *http.Request) string {
	layout := r.Header.Get("D-LAYOUT")
	if layout == "" && router.layoutResolver != nil {
		layout = router.layoutResolver(r)
		if layout == "" {
			slog.Debug("layout resolver returned empty string; rendering w/o layout")
		}
	} else if layout == "" {
		layout = router.config.getDefaultLayout()
	}
	if layout != "" {
		layout = strings.Join([]string{"layouts", layout}, "/")
		layoutTemplate := router.templates.Lookup(layout)
		if layoutTemplate == nil {
			slog.Debug("layout not found; rendering w/o layout", "layout", layout)
			layout = ""
		}
	}
	return layout
}

func (router *Router) parseForm(r *http.Request) *daveError {
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(router.config.getMaxFormSize()); err != nil {
			slog.Error("failed to parse multipart form", "error", err)
			return Unexpected(fmt.Errorf("failed to parse multipart form: %w", err))
		}
	} else {
		if err := r.ParseForm(); err != nil {
			slog.Error("failed to parse form", "error", err)
			return Unexpected(fmt.Errorf("failed to parse form: %w", err))
		}
	}
	return nil
}

func (router *Router) getFormHandler(r *http.Request) (FormHandlerFunc, *daveError) {
	router.parseForm(r)
	formHandlerKey := r.FormValue("d_form_handler")
	if formHandlerKey == "" {
		return nil, nil

	}
	slog.Info("executing form handler", "handler", formHandlerKey, "method", r.Method)
	handler := router.formHandlers[formHandlerKey]
	if handler == nil {
		slog.Error("no registered handler", "handler", formHandlerKey)
		return nil, Unexpected(fmt.Errorf("no registered handler: %s", formHandlerKey))
	}
	handlerMethod := handler[r.Method]
	if handlerMethod == nil {
		slog.Error("handler does not support method", "handler", formHandlerKey, "method", r.Method)
		return nil, Unexpected(fmt.Errorf("handler %s does not support method: %s", formHandlerKey, r.Method))
	}
	return handlerMethod, nil
}

func (router *Router) handleTemplateError(w http.ResponseWriter, r *http.Request, rootTemplate *template.Template, err error) {
	logger := LoggerFromContext(r.Context())
	daveErr := router.mapCustomErrorType(err)
	logger.Info("template error mapped to error type", "error_type", daveErr.message)
	t := rootTemplate.Lookup(daveErr.fallback)
	if t != nil {
		data := map[string]any{"error": daveErr.cause}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(daveErr.status)
		err = rootTemplate.ExecuteTemplate(w, daveErr.fallback, data)
		if err != nil {
			// log that something terrible has happened
		}
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(daveErr.status)
		w.Write([]byte(fmt.Sprintf("%s: %s", daveErr.message, daveErr.cause)))
	}
}

func (router *Router) renderError(w http.ResponseWriter, rootTemplate *template.Template, err error) {
	daveErr := router.mapCustomErrorType(err)
	slog.Info("returning error response", "error_type", daveErr.message, "cause", daveErr.cause)
	t := rootTemplate.Lookup(daveErr.fallback)
	if t != nil {
		data := map[string]any{"error": daveErr.cause}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(daveErr.status)
		err := rootTemplate.ExecuteTemplate(w, daveErr.fallback, data)
		if err != nil {
			slog.Error("error rendering fallback template", "template", daveErr.fallback, "cause", err)
		}
	} else {
		slog.Error("cannot find fallback template", "template", daveErr.fallback)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(daveErr.status)
		w.Write([]byte(fmt.Sprintf("%v", daveErr.cause)))
	}
	return
}

func (router *Router) ScanTemplates() error {
	slog.Info("scanning templates")
	rootTemplate := template.New(time.Now().String())

	placeholderFuncs := make(template.FuncMap)
	for name, factory := range router.templateFuncs {
		placeholderFuncs[name] = factory(nil)
	}
	rootTemplate.Funcs(placeholderFuncs)

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

func (router *Router) parseRequestPath(r *http.Request) (string, map[string]string, *daveError) {
	path := r.Header.Get("D-TEMPLATE")
	if path == "" {
		path = "index"
	}
	path = strings.TrimSuffix(r.URL.Path, "/") + "/" + path
	reqSegments := strings.Split(path[1:], "/")
	templatePath := ""
	pathVariables := make(map[string]string)
	bestSpecificity := -1
	ext := router.config.getTemplateExtension()

	for _, v := range router.templates.Templates() {
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
	if templatePath == "" {
		return "", nil, NotFound(fmt.Errorf("no template at %s", path))
	}
	return templatePath, pathVariables, nil
}

func stripTemplateSuffix(t string, ext string) string {
	i := strings.LastIndex(t, ext)
	if i < 0 {
		return t
	}
	return t[:i]
}

// GetRender retrieves the Render context from the request context.
// Use this in form handlers to access path variables, globals, and other render information.
func GetRender(context context.Context) Render {
	return context.Value(requestContextKey{}).(Render)
}

// PathVariable retrieves a path variable from the request context by name.
func PathVariable(r *http.Request, varName string) any {
	render := r.Context().Value(requestContextKey{}).(Render)
	return render.pathVariables[varName]
}

// GlobalValue retrieves a global value from the request context by name.
func GlobalValue(r *http.Request, name string) any {
	render := r.Context().Value(requestContextKey{}).(Render)
	return render.globals[name]
}

type daveError struct {
	message  string
	fallback string
	cause    error
	status   int
}

func (daveError daveError) Error() string {
	return daveError.message
}

func NotFound(cause error) *daveError {
	return &daveError{
		message:  "not found",
		fallback: "fallback/not_found",
		cause:    cause,
		status:   http.StatusNotFound,
	}
}

func Unexpected(cause error) *daveError {
	return &daveError{
		message:  "unexpected error",
		fallback: "fallback/unexpected_error",
		cause:    cause,
		status:   http.StatusInternalServerError,
	}
}

func (router *Router) mapCustomErrorType(err error) *daveError {
	var de *daveError
	if errors.As(err, &de) {
		return de
	}
	originalErr := err
	for {
		for _, et := range router.errorTypes {
			if err == et.target {
				return &daveError{
					message:  et.target.Error(),
					fallback: et.fallback,
					cause:    err,
					status:   et.status,
				}
			}
		}
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			break
		}
		err = unwrapped
	}
	return Unexpected(originalErr)
}

type guardedResponseWriter struct {
	http.ResponseWriter
	bodyWrote bool
}

func (g *guardedResponseWriter) Write(b []byte) (int, error) {
	if !g.bodyWrote {
		slog.Error("handler wrote to response body", "bytes", len(b))
		g.bodyWrote = true
	}
	// Ignore the write but return success to avoid errors in handler
	return len(b), nil
}
