package dave

import (
	"context"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"time"
)

type (
	FormHandlerConfFunc func(router *Router, varName string)
	FormHandlerFunc     func(w http.ResponseWriter, r *http.Request) (*Form, error)
	LayoutResolverFunc  func(r *http.Request) string
	ConfFunc            func(router *Router)
	MiddlewareFunc      func(next http.Handler) http.Handler
)

func (handlerFunc FormHandlerFunc) call(w http.ResponseWriter, r *http.Request) (*Form, bool, error) {
	tw := &trackingWriter{ResponseWriter: w}
	form, err := handlerFunc(tw, r)
	return form, tw.written, err
}

type errorTypeMapping struct {
	target   error
	status   int
	fallback string
}

type Router struct {
	fs             fs.FS
	formHandlers   map[string]map[string]FormHandlerFunc
	templateFuncs  map[string]func(*http.Request) any
	templates      *template.Template
	config         *Conf
	layoutResolver LayoutResolverFunc
	errorTypes     []errorTypeMapping
	middlewares    []MiddlewareFunc
}

type render struct {
	request       *http.Request
	template      string
	pathDir       string
	pathVariables map[string]string
	ctx           map[string]any
	form          *Form
	layout        string
}

type ctxValuesKey struct{}

type renderContextKey struct{}

type pathVariablesKey struct{}

type templateOverride struct {
	name string
}

type templateOverrideKey struct{}

func SetTemplate(r *http.Request, name string) {
	if o, ok := r.Context().Value(templateOverrideKey{}).(*templateOverride); ok {
		o.name = name
	}
}

var reservedKeys = map[string]bool{
	"path_variables": true,
	"form":           true,
	"error":          true,
	"content":        true,
}

// SetValue sets a context value that will be available in templates as {{.key}}.
// Panics if key is reserved. See documentation for reserved keys.
func SetValue(r *http.Request, key string, value any) *http.Request {
	if reservedKeys[key] {
		panic(fmt.Sprintf("dave: cannot use reserved key %q in SetValue", key))
	}
	values, _ := r.Context().Value(ctxValuesKey{}).(map[string]any)
	if values == nil {
		values = make(map[string]any)
		values[key] = value
		return r.WithContext(context.WithValue(r.Context(), ctxValuesKey{}, values))
	}
	values[key] = value
	return r
}

func GetValue(r *http.Request, key string) any {
	values, _ := r.Context().Value(ctxValuesKey{}).(map[string]any)
	if values == nil {
		return nil
	}
	return values[key]
}

func getCtxValues(r *http.Request) map[string]any {
	values, _ := r.Context().Value(ctxValuesKey{}).(map[string]any)
	if values == nil {
		return make(map[string]any)
	}
	return values
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
		return 10 << 20 // 10MB default
	}
	return c.MaxFormSize
}

func Func(name string, factory func(*http.Request) any) ConfFunc {
	return func(router *Router) {
		router.templateFuncs[name] = factory
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

func Middleware(mw MiddlewareFunc) ConfFunc {
	return func(router *Router) {
		router.middlewares = append(router.middlewares, mw)
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
		router.formHandlers[varName][m] = handler
	}
}

func NewRouter(fs fs.FS) *Router {
	return &Router{
		fs:            fs,
		formHandlers:  make(map[string]map[string]FormHandlerFunc),
		templateFuncs: make(map[string]func(*http.Request) any),
		config:        &Conf{},
	}
}

func (router *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if router.templates == nil || router.config.DevMode {
		if err := router.ScanTemplates(); err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusInternalServerError)
			if router.config.DevMode {
				w.Write([]byte(fmt.Sprintf("error scanning templates: %s", err)))
			} else {
				w.Write([]byte("internal server error"))
			}
			return
		}
	}

	render, daveErr := router.parseRequestPath(r)
	if daveErr != nil {
		rootTemplate, _ := router.templates.Clone()
		router.renderError(w, rootTemplate, daveErr)
		return
	}

	override := &templateOverride{}
	ctxValues := make(map[string]any)
	r = r.WithContext(context.WithValue(r.Context(), ctxValuesKey{}, ctxValues))
	r = r.WithContext(context.WithValue(r.Context(), pathVariablesKey{}, render.pathVariables))
	r = r.WithContext(context.WithValue(r.Context(), templateOverrideKey{}, override))

	handler := http.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		render.request = r
		render.ctx = getCtxValues(r)
		render.layout = router.getLayout(r)

		r = r.WithContext(context.WithValue(r.Context(), renderContextKey{}, render))

		rootTemplate, _ := router.templates.Clone()

		formHandler, daveErr := router.getFormHandler(r)
		if daveErr != nil {
			router.renderError(w, rootTemplate, daveErr)
			return
		}

		if formHandler != nil {
			form, wroteHTML, err := formHandler.call(w, r)
			if err != nil {
				router.renderError(w, rootTemplate, err)
				return
			}
			render.form = form
			if wroteHTML {
				return
			}
			if override.name != "" {
				render.template = router.resolveTemplate(override.name, render.pathDir)
			}
		}

		content, err := router.RenderTemplate(r, render.template)
		if err != nil {
			router.renderError(w, rootTemplate, err)
			return
		}

		if render.layout == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write(content)
			return
		}

		renderFuncs := make(template.FuncMap)
		for name, factory := range router.templateFuncs {
			renderFuncs[name] = factory(r)
		}
		rootTemplate.Funcs(renderFuncs)

		layoutData := getCtxValues(r)
		layoutData["content"] = template.HTML(content)
		pageWriter := &strings.Builder{}
		err = rootTemplate.ExecuteTemplate(pageWriter, render.layout, layoutData)
		if err != nil {
			router.renderError(w, rootTemplate, err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(pageWriter.String()))
	}))
	for i := len(router.middlewares) - 1; i >= 0; i-- {
		handler = router.middlewares[i](handler)
	}
	handler.ServeHTTP(w, r)
}

func (router *Router) getLayout(r *http.Request) string {
	var layout string
	if router.layoutResolver != nil {
		layout = router.layoutResolver(r)
	} else {
		layout = router.config.getDefaultLayout()
	}
	if layout != "" {
		layout = strings.Join([]string{"layouts", layout}, "/")
		layoutTemplate := router.templates.Lookup(layout)
		if layoutTemplate == nil {
			layout = ""
		}
	}
	return layout
}

func (router *Router) parseForm(r *http.Request) *daveError {
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		if err := r.ParseMultipartForm(router.config.getMaxFormSize()); err != nil {
			return BadRequest(fmt.Errorf("failed to parse multipart form: %w", err))
		}
	} else {
		if err := r.ParseForm(); err != nil {
			return BadRequest(fmt.Errorf("failed to parse form: %w", err))
		}
	}
	return nil
}

func (router *Router) getFormHandler(r *http.Request) (FormHandlerFunc, *daveError) {
	if err := router.parseForm(r); err != nil {
		return nil, err
	}
	formHandlerKey := r.FormValue("d_form_handler")
	if formHandlerKey == "" {
		return nil, nil
	}
	handler := router.formHandlers[formHandlerKey]
	if handler == nil {
		if router.config.DevMode {
			return nil, Unexpected(fmt.Errorf("no registered handler: %s", formHandlerKey))
		}
		return nil, Unexpected(fmt.Errorf("invalid form handler"))
	}
	handlerMethod := handler[r.Method]
	if handlerMethod == nil {
		if router.config.DevMode {
			return nil, Unexpected(fmt.Errorf("handler %s does not support method: %s", formHandlerKey, r.Method))
		}
		return nil, Unexpected(fmt.Errorf("invalid form handler"))
	}
	return handlerMethod, nil
}

func (router *Router) renderError(w http.ResponseWriter, rootTemplate *template.Template, err error) {
	daveErr := router.mapCustomErrorType(err)
	t := rootTemplate.Lookup(daveErr.fallback)
	if t != nil {
		data := map[string]any{"error": daveErr.cause}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(daveErr.status)
		rootTemplate.ExecuteTemplate(w, daveErr.fallback, data)
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(daveErr.status)
		if router.config.DevMode {
			w.Write([]byte(fmt.Sprintf("%v", daveErr.cause)))
		} else {
			w.Write([]byte(daveErr.message))
		}
	}
}

func (router *Router) ScanTemplates() error {
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
			scanErr = fmt.Errorf("failed to walk directory %s: %w", path, err)
			return scanErr
		}
		if path == "." {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ext) {
			return nil
		}
		newTemplate := rootTemplate.New(stripTemplateSuffix(path, ext))
		file, err := root.Open(path)
		if err != nil {
			scanErr = fmt.Errorf("failed to open template file %s: %w", path, err)
			return scanErr
		}
		defer file.Close()
		content, err := io.ReadAll(file)
		if err != nil {
			scanErr = fmt.Errorf("failed to read template file %s: %w", path, err)
			return scanErr
		}
		_, err = newTemplate.Parse(string(content))
		if err != nil {
			scanErr = fmt.Errorf("failed to parse template %s: %w", path, err)
			return scanErr
		}
		return nil
	})
	if scanErr != nil {
		return scanErr
	}
	router.templates = rootTemplate
	return nil
}

func (router *Router) RenderTemplate(r *http.Request, templateName string) ([]byte, error) {
	rend, _ := r.Context().Value(renderContextKey{}).(*render)

	rootTemplate, _ := router.templates.Clone()
	renderFuncs := make(template.FuncMap)
	for name, factory := range router.templateFuncs {
		renderFuncs[name] = factory(r)
	}
	rootTemplate.Funcs(renderFuncs)

	t := rootTemplate.Lookup(templateName)
	if t == nil {
		return nil, fmt.Errorf("template not found: %s", templateName)
	}

	data := getCtxValues(r)
	if rend != nil {
		data["path_variables"] = rend.pathVariables
		if rend.form != nil {
			data["form"] = rend.form
		}
	}

	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	return []byte(buf.String()), nil
}

func (router *Router) parseRequestPath(r *http.Request) (*render, *daveError) {
	path := r.Header.Get("D-TEMPLATE")
	if path == "" {
		path = "index"
	} else if !isValidTemplateName(path) {
		return nil, NotFound(fmt.Errorf("invalid template name"))
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
		return nil, NotFound(fmt.Errorf("no template at %s", path))
	}
	pathDir := ""
	if idx := strings.LastIndex(templatePath, "/"); idx >= 0 {
		pathDir = templatePath[:idx]
	}
	return &render{
		template:      templatePath,
		pathDir:       pathDir,
		pathVariables: pathVariables,
	}, nil
}

func stripTemplateSuffix(t string, ext string) string {
	i := strings.LastIndex(t, ext)
	if i < 0 {
		return t
	}
	return t[:i]
}

var validTemplateNamePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func isValidTemplateName(name string) bool {
	return validTemplateNamePattern.MatchString(name)
}

func PathVariable(r *http.Request, varName string) string {
	if pv, ok := r.Context().Value(pathVariablesKey{}).(map[string]string); ok {
		return pv[varName]
	}
	return ""
}

func PathVariables(r *http.Request) map[string]string {
	if pv, ok := r.Context().Value(pathVariablesKey{}).(map[string]string); ok {
		return pv
	}
	return nil
}

func Template(r *http.Request) string {
	render, ok := r.Context().Value(renderContextKey{}).(*render)
	if !ok || render == nil {
		return ""
	}
	return render.template
}

func Layout(r *http.Request) string {
	rend, ok := r.Context().Value(renderContextKey{}).(*render)
	if !ok || rend == nil {
		return ""
	}
	return rend.layout
}

func FormResponse(r *http.Request) *Form {
	rend, ok := r.Context().Value(renderContextKey{}).(*render)
	if !ok || rend == nil {
		return nil
	}
	return rend.form
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

func BadRequest(cause error) *daveError {
	return &daveError{
		message:  "bad request",
		fallback: "fallback/bad_request",
		cause:    cause,
		status:   http.StatusBadRequest,
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

type trackingWriter struct {
	http.ResponseWriter
	written bool
}

func (tw *trackingWriter) Write(b []byte) (int, error) {
	if len(b) > 0 {
		tw.written = true
		tw.ResponseWriter.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	return tw.ResponseWriter.Write(b)
}

func (router *Router) resolveTemplate(name, pathDir string) string {
	if pathDir != "" {
		relativePath := pathDir + "/" + name
		if router.templates.Lookup(relativePath) != nil {
			return relativePath
		}
	}
	return name
}
